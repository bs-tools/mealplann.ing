import Loading from "@components/loading";
import ApplyTemplateDialog from "@components/templates/apply-template-dialog";
import BackLink from "@components/ui/back-link";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { TextAreaField, TextField } from "@components/ui/fields";
import { Badge, PageHeader } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  iconButton,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  CalendarDaysIcon,
  PencilIcon,
  TrashIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import type { EatingTime, Person } from "gen/people/v1/people_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import {
  deletePlanTemplate,
  getPlanTemplate,
  updatePlanTemplate,
} from "gen/plans/v1/templates-PlanTemplateService_connectquery";
import type {
  PlanTemplate,
  TemplateBatch,
  TemplateEntry,
  TemplateSlot,
} from "gen/plans/v1/templates_pb";
import { PlanTemplateService } from "gen/plans/v1/templates_pb";
import { formatShare } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";

/**
 * EatingTimeFor returns the eating time of a person a template's slot maps
 * to, like the server: the one of its shared meal, or else the one of its
 * kind in the same position among those without a shared meal.
 */
const eatingTimeFor = (
  slots: TemplateSlot[],
  slot: TemplateSlot,
  person: Person
): EatingTime | undefined => {
  if (slot.meal !== HouseholdMeal.UNSPECIFIED) {
    return person.eatingTimes.find((et) => et.meal === slot.meal);
  }
  const position = slots
    .slice(0, slots.indexOf(slot))
    .filter(
      (other) =>
        other.meal === HouseholdMeal.UNSPECIFIED && other.kind === slot.kind
    ).length;
  return person.eatingTimes.filter(
    (et) => et.meal === HouseholdMeal.UNSPECIFIED && et.kind === slot.kind
  )[position];
};

/**
 * EntryEaters says who eats an entry, and how much, e.g. "Anna 0.52 · Ben
 * 1 · Charlotte ⅓", or "Nobody" if nobody eats it.
 */
const entryEaters = (
  template: PlanTemplate,
  entry: TemplateEntry,
  people: Person[]
): string => {
  const slot = template.slots.find((other) => other.key === entry.slotKey);
  const parts: string[] = [];
  for (const person of people) {
    const own = entry.personServings.find(
      (other) => other.personId === person.id
    );
    const servings = own === undefined ? entry.servings : own.servings;
    if (
      servings > 0 &&
      slot !== undefined &&
      eatingTimeFor(template.slots, slot, person) !== undefined
    ) {
      parts.push(`${person.name} ${formatShare(servings)}`);
    }
  }
  return parts.length === 0 ? "Nobody" : parts.join(" · ");
};

// BatchNote says how an entry is cooked: as a batch, or from one.
const batchNote = (
  entry: TemplateEntry,
  batches: TemplateBatch[]
): string | undefined => {
  const batch = batches.find((other) => other.entryKeys.includes(entry.key));
  if (batch === undefined) {
    return undefined;
  }
  if (batch.day === entry.day && batch.entryKeys[0] === entry.key) {
    return `Batch of ${batch.servings > 0 ? batch.servings.toLocaleString() : "its"} servings`;
  }
  return `From day ${batch.day + 1}'s batch`;
};

interface DetailsDialogProps {
  template: PlanTemplate;
  onClose: () => void;
}

// DetailsDialog edits a template's name, source and notes.
const DetailsDialog: React.FC<DetailsDialogProps> = ({ template, onClose }) => {
  const queryClient = useQueryClient();
  const [name, setName] = useState(template.name);
  const [source, setSource] = useState(template.source);
  const [notes, setNotes] = useState(template.notes);
  const update = useMutation(updatePlanTemplate, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanTemplateService]);
      onClose();
    },
  });
  const dirty =
    name !== template.name ||
    source !== template.source ||
    notes !== template.notes;
  return (
    <Modal open title="Edit template" dirty={dirty} onClose={onClose}>
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          update.mutate({
            template: { id: template.id, name, notes, source },
            updateMask: { paths: ["name", "source", "notes"] },
          });
        }}
      >
        <TextField label="Name" value={name} required onChange={setName} />
        <TextField label="Source" value={source} onChange={setSource} />
        <TextAreaField label="Notes" value={notes} onChange={setNotes} />
        <ErrorText error={update.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={!dirty || name.trim() === "" || update.isPending}
          >
            {update.isPending ? "Saving…" : "Save"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

/**
 * PlanTemplatePage shows a template: each day's meals by slot, who eats
 * them, and which are cooked in batches, to plan it, edit it or delete it.
 */
const PlanTemplatePage: React.FC = () => {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const template = useQuery(getPlanTemplate, { id });
  const people = useQuery(listPeople, {});
  const [applying, setApplying] = useState(false);
  const [editing, setEditing] = useState(false);
  const onChanged = async (): Promise<void> => {
    await invalidateServices(queryClient, [PlanTemplateService]);
  };
  const update = useMutation(updatePlanTemplate, { onSuccess: onChanged });
  const remove = useMutation(deletePlanTemplate, {
    onSuccess: async () => {
      await onChanged();
      void navigate("/plan/templates");
    },
  });

  if (template.isPending) {
    return <Loading />;
  }
  if (template.isError) {
    return (
      <QueryError
        error={template.error}
        onRetry={() => {
          void template.refetch();
        }}
      />
    );
  }
  const tmpl = template.data.template;
  if (tmpl === undefined) {
    return null;
  }
  const everyone = people.data?.people ?? [];
  // Removing an entry removes it from its batch, and an empty batch.
  const removeEntry = (key: string) => {
    update.mutate({
      template: {
        batches: tmpl.batches
          .map((batch) => ({
            ...batch,
            entryKeys: batch.entryKeys.filter((other) => other !== key),
          }))
          .filter((batch) => batch.entryKeys.length > 0),
        entries: tmpl.entries.filter((entry) => entry.key !== key),
        id: tmpl.id,
      },
      updateMask: { paths: ["entries", "batches"] },
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <BackLink to="/plan/templates">Templates</BackLink>
      <PageHeader
        title={tmpl.name}
        subtitle={tmpl.source === "" ? undefined : tmpl.source}
        actions={
          <div className="flex flex-wrap gap-2 print:hidden">
            <ConfirmButton
              title="Delete template?"
              message={`"${tmpl.name}" will be deleted. What you planned from it stays.`}
              disabled={remove.isPending}
              onConfirm={() => {
                remove.mutate({ id: tmpl.id });
              }}
            >
              <TrashIcon className="size-4" aria-hidden="true" />
              Delete
            </ConfirmButton>
            <button
              type="button"
              className={secondaryButton}
              onClick={() => {
                setEditing(true);
              }}
            >
              <PencilIcon className="size-4" aria-hidden="true" />
              Edit
            </button>
            <button
              type="button"
              className={primaryButton}
              disabled={people.data === undefined}
              onClick={() => {
                setApplying(true);
              }}
            >
              <CalendarDaysIcon className="size-4" aria-hidden="true" />
              Plan it
            </button>
          </div>
        }
      />
      <ErrorText error={update.error ?? remove.error} />
      {tmpl.notes !== "" && (
        <p className="text-sm whitespace-pre-line text-gray-700">
          {tmpl.notes}
        </p>
      )}
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: tmpl.days }, (_unused, day) => {
          const entries = tmpl.entries.filter((entry) => entry.day === day);
          return (
            <Card key={day} title={`Day ${day + 1}`}>
              {entries.length === 0 ? (
                <p className="text-sm text-gray-500">Nothing planned.</p>
              ) : (
                <ul className="flex flex-col gap-3">
                  {tmpl.slots
                    .filter((slot) =>
                      entries.some((entry) => entry.slotKey === slot.key)
                    )
                    .map((slot) => (
                      <li key={slot.key} className="flex flex-col gap-1">
                        <span className="text-[11px] font-semibold tracking-wide text-gray-500 uppercase">
                          {slot.name}
                        </span>
                        <ul className="flex flex-col gap-1">
                          {entries
                            .filter((entry) => entry.slotKey === slot.key)
                            .map((entry) => {
                              const note = batchNote(entry, tmpl.batches);
                              return (
                                <li
                                  key={entry.key}
                                  className="flex items-start justify-between gap-2"
                                >
                                  <div className="flex min-w-0 flex-col">
                                    {entry.recipeName === "" ? (
                                      <span className="text-sm text-amber-800">
                                        A deleted recipe
                                      </span>
                                    ) : (
                                      <Link
                                        to={`/recipes/${entry.recipeId}`}
                                        className="text-sm font-medium text-gray-900 hover:underline"
                                      >
                                        {entry.recipeName}
                                      </Link>
                                    )}
                                    <span className="text-xs text-gray-600">
                                      {entryEaters(tmpl, entry, everyone)}
                                    </span>
                                    {note !== undefined && (
                                      <span className="mt-0.5">
                                        <Badge>{note}</Badge>
                                      </span>
                                    )}
                                  </div>
                                  <button
                                    type="button"
                                    className={`${iconButton} shrink-0 print:hidden`}
                                    aria-label="Remove"
                                    title="Remove this meal from the template"
                                    disabled={update.isPending}
                                    onClick={() => {
                                      removeEntry(entry.key);
                                    }}
                                  >
                                    <XMarkIcon className="size-4" />
                                  </button>
                                </li>
                              );
                            })}
                        </ul>
                      </li>
                    ))}
                </ul>
              )}
            </Card>
          );
        })}
      </div>
      {applying && (
        <ApplyTemplateDialog
          template={tmpl}
          people={everyone}
          onClose={() => {
            setApplying(false);
          }}
        />
      )}
      {editing && (
        <DetailsDialog
          template={tmpl}
          onClose={() => {
            setEditing(false);
          }}
        />
      )}
    </div>
  );
};

export default PlanTemplatePage;
