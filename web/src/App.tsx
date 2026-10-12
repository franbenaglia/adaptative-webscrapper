import { useRoute } from "./router";
import { JobDetail } from "./views/JobDetail";
import { Jobs } from "./views/Jobs";
import { NewJob } from "./views/NewJob";
import { RecipeEditor } from "./views/RecipeEditor";
import { Recipes } from "./views/Recipes";

export function App() {
  const route = useRoute();
  const section = route.view === "job" ? "jobs" : route.view === "recipe" ? "recipes" : route.view;

  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <a className="brand" href="#/">
            AdaptiveScraper
          </a>
          <nav aria-label="Secciones">
            <a href="#/" aria-current={section === "new" ? "page" : undefined}>
              Nueva extracción
            </a>
            <a href="#/jobs" aria-current={section === "jobs" ? "page" : undefined}>
              Trabajos
            </a>
            <a href="#/recipes" aria-current={section === "recipes" ? "page" : undefined}>
              Recetas
            </a>
          </nav>
        </div>
      </header>
      <main>
        {route.view === "new" && <NewJob recipeId={route.recipeId} />}
        {route.view === "jobs" && <Jobs />}
        {route.view === "job" && <JobDetail key={route.id} id={route.id} />}
        {route.view === "recipes" && <Recipes />}
        {route.view === "recipe" && <RecipeEditor key={route.id ?? "new"} id={route.id} />}
      </main>
    </>
  );
}
