import Loading from "@components/loading";
import ApplyTemplateDialog from "@components/templates/apply-template-dialog";
import SaveTemplateDialog from "@components/templates/save-template-dialog";
import BackLink from "@components/ui/back-link";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import {
  cardClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  BookmarkSquareIcon,
  CalendarDaysIcon,
} from "@heroicons/react/24/outline";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import { listPlanTemplates } from "gen/plans/v1/templates-PlanTemplateService_connectquery";
import type { PlanTemplateSummary } from "gen/plans/v1/templates_pb";
import { isDateString } from "helpers/dates";
import { formatList } from "helpers/format";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

// SHOWN_RECIPES is how many of a template's recipes its card names.
const SHOWN_RECIPES = 4;

/**
 * PlanTemplatesPage lists the household's plan templates, to plan one from
 * a date, and saves days of the plan as a new one.
 */
const PlanTemplatesPage: React.FC = () => {
  const [params] = useSearchParams();
  const templates = useQuery(listPlanTemplates, {});
  const people = useQuery(listPeople, {});
  const [saving, setSaving] = useState(false);
  const [applying, setApplying] = useState<PlanTemplateSummary | null>(null);
  // The plan links here with the week it shows, to save it.
  const week = params.get("week") ?? "";

  let content: React.ReactNode = null;
  if (templates.isPending) {
    content = <Loading />;
  } else if (templates.isError) {
    content = (
      <QueryError
        error={templates.error}
        onRetry={() => {
          void templates.refetch();
        }}
      />
    );
  } else if (templates.data.templates.length === 0) {
    content = (
      <EmptyState title="No templates yet">
        Save a week you planned as a template, to plan it again from any date,
        or ask your AI agent to make one from a meal plan.
      </EmptyState>
    );
  } else {
    content = (
      <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {templates.data.templates.map((template) => {
          const recipes = template.recipeNames.slice(0, SHOWN_RECIPES);
          const more = template.recipeNames.length - recipes.length;
          return (
            <li
              key={template.id}
              className={`${cardClass} flex flex-col gap-2 p-4`}
            >
              <div>
                <Link
                  to={`/plan/templates/${template.id}`}
                  className="font-semibold text-ink-soft hover:underline"
                >
                  {template.name}
                </Link>
                {template.source !== "" && (
                  <p className="text-sm text-muted">{template.source}</p>
                )}
              </div>
              <p className="text-xs text-muted">
                {template.days} {template.days === 1 ? "day" : "days"} ·{" "}
                {template.entryCount}{" "}
                {template.entryCount === 1 ? "meal" : "meals"}
              </p>
              {recipes.length > 0 && (
                <p className="text-sm text-ink-soft">
                  {formatList(
                    more > 0 ? [...recipes, `${more} more`] : recipes
                  )}
                </p>
              )}
              <div className="mt-auto flex gap-2 pt-1">
                <button
                  type="button"
                  className={primaryButton}
                  disabled={people.data === undefined}
                  onClick={() => {
                    setApplying(template);
                  }}
                >
                  <CalendarDaysIcon className="size-4" aria-hidden="true" />
                  Plan it
                </button>
                <Link
                  to={`/plan/templates/${template.id}`}
                  className={secondaryButton}
                >
                  View
                </Link>
              </div>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <BackLink to="/plan">Plan</BackLink>
      <PageHeader
        title="Templates"
        subtitle="Plans to use again: everyone's portions and the leftovers, from any date."
        actions={
          <button
            type="button"
            className={secondaryButton}
            onClick={() => {
              setSaving(true);
            }}
          >
            <BookmarkSquareIcon className="size-4" aria-hidden="true" />
            Save days as a template
          </button>
        }
      />
      {content}
      {saving && (
        <SaveTemplateDialog
          start={isDateString(week) ? week : undefined}
          onClose={() => {
            setSaving(false);
          }}
        />
      )}
      {applying !== null && (
        <ApplyTemplateDialog
          template={applying}
          people={people.data?.people ?? []}
          onClose={() => {
            setApplying(null);
          }}
        />
      )}
    </div>
  );
};

export default PlanTemplatesPage;
