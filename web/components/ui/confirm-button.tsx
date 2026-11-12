import type { ReactNode } from "react";
import React, { useState } from "react";

import Modal from "./modal";
import { dangerButton, secondaryButton, secondaryDangerButton } from "./styles";

interface ConfirmButtonProps {
  // The button content.
  children: ReactNode;
  className?: string;
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  disabled?: boolean;
  onConfirm: () => void;
}

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  onClose: () => void;
  onConfirm: () => void;
}

/**
 * ConfirmDialog asks for confirmation before a destructive action, for one
 * started from somewhere other than a button of its own, like a ⋯ menu.
 */
export const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  open,
  title,
  message,
  confirmLabel = "Delete",
  onClose,
  onConfirm,
}) => (
  <Modal open={open} onClose={onClose} title={title}>
    <div className="flex flex-col gap-4">
      <div className="text-sm text-ink-soft">{message}</div>
      <div className="flex justify-end gap-2">
        <button type="button" className={secondaryButton} onClick={onClose}>
          Cancel
        </button>
        <button
          type="button"
          className={dangerButton}
          onClick={() => {
            onClose();
            onConfirm();
          }}
        >
          {confirmLabel}
        </button>
      </div>
    </div>
  </Modal>
);

// ConfirmButton asks for confirmation before running a destructive action.
const ConfirmButton: React.FC<ConfirmButtonProps> = ({
  children,
  className = secondaryDangerButton,
  title,
  message,
  confirmLabel = "Delete",
  disabled = false,
  onConfirm,
}) => {
  const [open, setOpen] = useState(false);
  const close = () => {
    setOpen(false);
  };
  return (
    <>
      <button
        type="button"
        className={className}
        disabled={disabled}
        onClick={() => {
          setOpen(true);
        }}
      >
        {children}
      </button>
      <ConfirmDialog
        open={open}
        title={title}
        message={message}
        confirmLabel={confirmLabel}
        onClose={close}
        onConfirm={onConfirm}
      />
    </>
  );
};

export default ConfirmButton;
