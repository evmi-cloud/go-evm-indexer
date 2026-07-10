/** @type {import('next').NextConfig} */
const nextConfig = {
  // Emit a fully static site to ./out so the Go server can serve it as files.
  output: "export",
  // Each route becomes <route>/index.html, which plain file serving handles well.
  trailingSlash: true,
  // The static export has no image optimization server.
  images: { unoptimized: true },
};

module.exports = nextConfig;
