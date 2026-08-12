import { mkdirSync, writeFileSync } from "node:fs";

const placeholder = "frontend/dist/.gitkeep";
mkdirSync("frontend/dist", { recursive: true });
writeFileSync(placeholder, "# Keep frontend/dist present before the first Vite build so go:embed compiles.\n");
