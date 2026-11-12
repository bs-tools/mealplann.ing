import Loading from "@components/loading";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { SelectField } from "@components/ui/fields";
import { CopyField } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  ghostDangerButton,
  linkClass,
  primaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import {
  createCalendarFeed,
  deleteCalendarFeed,
  getCalendarFeed,
  updateCalendarFeed,
} from "gen/users/v1/users-UserService_connectquery";
import { UserService } from "gen/users/v1/users_pb";
import { formatTimestamp } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

// BrowserTimeZone returns the browser's time zone, like "Europe/Berlin".
const browserTimeZone = (): string =>
  Intl.DateTimeFormat().resolvedOptions().timeZone;

/**
 * TimeZones returns the time zones to choose from: those the browser
 * knows, with the current one, which older browsers may not list.
 */
const timeZones = (current: string): string[] => {
  const known = Intl.supportedValuesOf("timeZone");
  return known.includes(current) ? known : [current, ...known];
};

/**
 * CalendarFeed makes a calendar feed: a secret link a calendar app, like
 * Google Calendar or Apple Calendar, subscribes to, with the cooks, prep
 * and meals of the user's days. The link is shown once, as only a hash of
 * it is kept; a new one stops the old one working.
 */
const CalendarFeed: React.FC = () => {
  const queryClient = useQueryClient();
  const feed = useQuery(getCalendarFeed);
  // The link of the feed just made, shown until the page is left.
  const [url, setUrl] = useState("");
  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [UserService]);
  };
  const create = useMutation(createCalendarFeed, {
    onSuccess: async (response) => {
      setUrl(response.feed?.url ?? "");
      await onSuccess();
    },
  });
  const update = useMutation(updateCalendarFeed, { onSuccess });
  const remove = useMutation(deleteCalendarFeed, {
    onSuccess: async () => {
      setUrl("");
      await onSuccess();
    },
  });

  let body: React.ReactNode = null;
  if (feed.isPending) {
    body = <Loading />;
  } else if (feed.isError) {
    body = (
      <QueryError
        error={feed.error}
        onRetry={() => {
          void feed.refetch();
        }}
      />
    );
  } else if (feed.data.feed === undefined) {
    body = (
      <button
        type="button"
        className={`${primaryButton} self-start`}
        disabled={create.isPending}
        onClick={() => {
          create.mutate({ timeZone: browserTimeZone() });
        }}
      >
        Make a calendar link
      </button>
    );
  } else {
    const { feed: current } = feed.data;
    body = (
      <div className="flex flex-col gap-3">
        {url === "" ? (
          <p className="text-sm text-muted">
            Your calendar link was made {formatTimestamp(current.createTime)}
            {current.lastUsedTime === undefined
              ? ", and no calendar has read it yet."
              : `, and a calendar last read it ${formatTimestamp(current.lastUsedTime)}.`}{" "}
            It&apos;s only shown once: make a new one to add it to another
            calendar, which stops the old one working.
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            <p className="text-sm text-ink-soft">
              Copy this link into your calendar app, as a calendar to subscribe
              to by URL. It&apos;s only shown now: keep it to yourself, as
              anyone with it can see your meals.
            </p>
            <CopyField value={url} />
            <a
              href={url.replace(/^https?:/u, "webcal:")}
              className={`${linkClass} self-start text-sm`}
            >
              Open it in your calendar app
            </a>
          </div>
        )}
        <SelectField
          label="Time zone of your meals"
          value={current.timeZone}
          options={timeZones(current.timeZone).map((zone) => ({
            label: zone.replaceAll("_", " "),
            value: zone,
          }))}
          onChange={(timeZone) => {
            update.mutate({ timeZone });
          }}
        />
        {current.timeZone !== browserTimeZone() && (
          <p className="-mt-1 text-xs text-muted">
            This browser is in {browserTimeZone().replaceAll("_", " ")}.
          </p>
        )}
        <div className="-ml-2 flex flex-wrap gap-1">
          <ConfirmButton
            className={ghostButton}
            title="Make a new calendar link?"
            message="The old link stops working: calendars using it stop updating until you give them the new one."
            confirmLabel="Make a new link"
            disabled={create.isPending}
            onConfirm={() => {
              create.mutate({ timeZone: current.timeZone });
            }}
          >
            Make a new link
          </ConfirmButton>
          <ConfirmButton
            className={ghostDangerButton}
            title="Stop the calendar link?"
            message="Calendars using it stop updating, and keep only what they had."
            confirmLabel="Stop it"
            disabled={remove.isPending}
            onConfirm={() => {
              remove.mutate({});
            }}
          >
            Stop it
          </ConfirmButton>
        </div>
      </div>
    );
  }

  return (
    <Card title="Calendar">
      <div className="flex flex-col gap-3">
        <p className="text-sm text-muted">
          See when to start cooking, what to prep ahead and your meals in your
          own calendar, for this week and the next two, kept up to date.
        </p>
        {body}
        <ErrorText error={create.error ?? update.error ?? remove.error} />
      </div>
    </Card>
  );
};

export default CalendarFeed;
