import { useEffect, useState } from "react";

// Rutas por hash: funcionan con cualquier servidor de archivos estáticos.
export type Route =
  | { view: "new"; recipeId?: string }
  | { view: "jobs" }
  | { view: "job"; id: string }
  | { view: "recipes" }
  | { view: "recipe"; id?: string };

export function parseRoute(hash: string): Route {
  const [path, query = ""] = hash.replace(/^#\/?/, "").split("?");
  const parts = path.split("/").filter(Boolean).map(decodeURIComponent);
  switch (parts[0]) {
    case "jobs":
      return parts[1] ? { view: "job", id: parts[1] } : { view: "jobs" };
    case "recipes":
      if (parts[1] === "new") return { view: "recipe" };
      return parts[1] ? { view: "recipe", id: parts[1] } : { view: "recipes" };
    default:
      return { view: "new", recipeId: new URLSearchParams(query).get("recipe") ?? undefined };
  }
}

export function useRoute(): Route {
  const [hash, setHash] = useState(window.location.hash);
  useEffect(() => {
    const onChange = () => setHash(window.location.hash);
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);
  return parseRoute(hash);
}

export function navigate(hash: string): void {
  window.location.hash = hash;
}
