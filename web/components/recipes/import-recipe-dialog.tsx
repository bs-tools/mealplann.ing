import { TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import { primaryButton, secondaryButton } from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { importRecipe } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { useNavigate } from "react-router";

interface ImportRecipeDialogProps {
  open: boolean;
  onClose: () => void;
}

/**
 * ImportRecipeDialog imports a recipe from a web page, like a food blog's,
 * as a draft, and opens it to edit: to choose the foods of the ingredients
 * that weren't matched, and save it.
 */
const ImportRecipeDialog: React.FC<ImportRecipeDialogProps> = ({
  open,
  onClose,
}) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [url, setUrl] = useState("");
  const importing = useMutation(importRecipe, {
    onSuccess: async (response) => {
      await invalidateServices(queryClient, [RecipeService]);
      void navigate(`/recipes/${response.recipe?.id ?? ""}/edit`);
    },
  });
  return (
    <Modal open={open} title="Import a recipe" onClose={onClose}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          importing.mutate({ url: url.trim() });
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
        <p className="text-sm text-gray-600">
          Most recipe sites, like food blogs, describe their recipes for search
          engines. It&apos;s saved as a draft for you to check: choose the foods
          of the ingredients it couldn&apos;t match, then save it.
        </p>
        <ErrorText error={importing.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={importing.isPending || url.trim() === ""}
          >
            {importing.isPending ? "Importing…" : "Import"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default ImportRecipeDialog;
