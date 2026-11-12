import { TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { primaryButton, secondaryButton } from "@components/ui/styles";
import React, { useState } from "react";
import { useNavigate } from "react-router";

interface ImportRecipeDialogProps {
  open: boolean;
  onClose: () => void;
}

/**
 * ImportRecipeDialog asks for a recipe's web page, like a food blog's, and
 * opens it on the import page, to choose the foods of its ingredients.
 */
const ImportRecipeDialog: React.FC<ImportRecipeDialogProps> = ({
  open,
  onClose,
}) => {
  const navigate = useNavigate();
  const [url, setUrl] = useState("");
  return (
    <Modal open={open} title="Import a recipe" onClose={onClose}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          void navigate(
            `/recipes/import?url=${encodeURIComponent(url.trim())}`
          );
        }}
      >
        <TextField
          label="Address of the recipe's page"
          type="url"
          value={url}
          placeholder="https://"
          required
          autoFocus
          onChange={setUrl}
        />
        <p className="text-sm text-muted">
          Most recipe sites, like food blogs, describe their recipes for search
          engines. You then choose the food of each ingredient, from those most
          like it, before saving it.
        </p>
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={url.trim() === ""}
          >
            Read recipe
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default ImportRecipeDialog;
