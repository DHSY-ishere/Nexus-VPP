import type { NextConfig } from "next";
import path from "path";

const nextConfig: NextConfig = {
  // The repo root also has a package-lock.json; without this Turbopack picks it as
  // the workspace root and cannot resolve tailwindcss from frontend/node_modules.
  turbopack: {
    root: path.join(__dirname),
  },
};

export default nextConfig;
