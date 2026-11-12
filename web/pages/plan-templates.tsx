import Loading from "@components/loading";
import ApplyTemplateDialog from "@components/templates/apply-template-dialog";
import SaveTemplateDialog from "@components/templates/save-template-dialog";
import BackLink from "@components/ui/back-link";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import { ghostButton } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { PlusIcon } from "@heroicons/react/24/outline";
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
      <ul className="divide-y divide-line rounded-2xl border border-line bg-white px-4">
        {templates.data.templates.map((template) => {
          const recipes = template.recipeNames.slice(0, SHOWN_RECIPES);
          const more = template.recipeNames.length - recipes.length;
          return (
            <li key={template.id} className="flex items-center gap-3 py-3.5">
              <Link
                to={`/plan/templates/${template.id}`}
                className="flex min-w-0 grow flex-col gap-0.5"
              >
                <span className="font-bold text-ink-soft">{template.name}</span>
                <span className="text-sm text-muted">
                  {[
                    template.source,
                    `${template.days} ${template.days === 1 ? "day" : "days"}`,
                    `${template.entryCount} ${template.entryCount === 1 ? "meal" : "meals"}`,
                  ]
                    .filter((part) => part !== "")
                    .join(" · ")}
                </span>
                {recipes.length > 0 && (
                  <span className="line-clamp-2 text-sm text-ink-soft">
                    {formatList(
                      more > 0 ? [...recipes, `${more} more`] : recipes
                    )}
                  </span>
                )}
              </Link>
              <button
                type="button"
                className="shrink-0 rounded-full bg-pine-50 px-3.5 py-2 text-sm font-bold text-pine hover:bg-pine-100 disabled:opacity-50"
                disabled={people.data === undefined}
                onClick={() => {
                  setApplying(template);
                }}
              >
                Plan it
              </button>
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
        subtitle="Plans to use again, like a dietitian's sample week: everyone's portions and the leftovers, from any date."
      />
      <button
        type="button"
        className={`${ghostButton} -mt-3 -ml-2 self-start text-pine`}
        onClick={() => {
          setSaving(true);
        }}
      >
        <PlusIcon className="size-4" aria-hidden="true" />
        Save days of the plan as a template
      </button>
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
