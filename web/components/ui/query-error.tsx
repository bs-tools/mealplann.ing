import type { ConnectError } from "@connectrpc/connect";
import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import React from "react";

import { secondaryButton } from "./styles";

interface QueryErrorProps {
  error: ConnectError;
  onRetry?: () => void;
}

// QueryError shows a failed request with an optional retry button.
const QueryError: React.FC<QueryErrorProps> = ({ error, onRetry }) => (
  <div className="flex items-start gap-3 rounded-md border border-error/20 bg-error-tint p-3 text-sm text-error">
    <ExclamationTriangleIcon className="size-5 shrink-0" />
    <div className="flex grow flex-col gap-2">
      <p>{error.rawMessage === "" ? error.message : error.rawMessage}</p>
      {onRetry !== undefined && (
        <div>
          <button type="button" className={secondaryButton} onClick={onRetry}>
            Retry
          </button>
        </div>
      )}
    </div>
  </div>
);

interface ErrorTextProps {
  error: ConnectError | null;
}

// ErrorText shows a mutation error inline, if any.
export const ErrorText: React.FC<ErrorTextProps> = ({ error }) => {
  if (error === null) {
    return null;
  }
  return (
    <p className="text-sm text-error">
      {error.rawMessage === "" ? error.message : error.rawMessage}
    </p>
  );
};

export default QueryError;
