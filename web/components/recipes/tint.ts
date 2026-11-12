// TINTS are the soft colors of recipes' cards, in place of a photo.
const TINTS = [
  "bg-[#f2cdb8]",
  "bg-[#d4e5c6]",
  "bg-[#f0e1b4]",
  "bg-[#dcd6ef]",
  "bg-[#cfe3ec]",
  "bg-[#ecd2d6]",
];

// TintOf picks a recipe's tint from its ID, so it keeps the same one.
export const tintOf = (id: string): string => {
  let hash = 0;
  for (const char of id) {
    hash = (hash * 31 + (char.codePointAt(0) ?? 0)) % 1_000_003;
  }
  return TINTS[hash % TINTS.length] ?? "bg-ground-dark";
};
