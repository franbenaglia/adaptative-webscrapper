import type { Recipe } from "./api";

export const RECIPE_TEMPLATE = `{
  "name": "Mi receta",
  "start_url": "https://example.com/productos",
  "item_locator": ".producto",
  "fields": [
    {"name": "nombre", "type": "text", "locator": ".nombre", "required": true},
    {"name": "precio", "type": "number", "locator": ".precio"},
    {"name": "enlace", "type": "url", "locator": "a", "attribute": "href"}
  ],
  "pagination": {"type": "next_button", "next_locator": "a.siguiente"}
}`;

export type ParsedRecipe = { ok: true; recipe: Recipe } | { ok: false; error: string };

// parseRecipe solo comprueba que el texto sea un objeto JSON; las reglas de
// la receta las valida el servidor, que es la única fuente de verdad.
export function parseRecipe(text: string): ParsedRecipe {
  if (text.trim() === "") {
    return { ok: false, error: "La receta está vacía." };
  }
  try {
    const value: unknown = JSON.parse(text);
    if (value === null || typeof value !== "object" || Array.isArray(value)) {
      return { ok: false, error: "La receta debe ser un objeto JSON." };
    }
    return { ok: true, recipe: value as Recipe };
  } catch (err) {
    return { ok: false, error: `JSON inválido: ${err instanceof Error ? err.message : String(err)}` };
  }
}

// editableRecipe quita los campos que asigna el servidor.
export function editableRecipe(recipe: Recipe): string {
  const { id: _id, version: _version, ...rest } = recipe;
  return JSON.stringify(rest, null, 2);
}
