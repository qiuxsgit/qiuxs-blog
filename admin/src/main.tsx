import { createRoot } from "react-dom/client";
import "@milkdown/kit/prose/view/style/prosemirror.css";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/components.css";

import { AppProviders } from "./app/AppProviders";
import { AppRouter } from "./app/AppRouter";

createRoot(document.getElementById("root")!).render(
  <AppProviders>
    <AppRouter />
  </AppProviders>,
);
