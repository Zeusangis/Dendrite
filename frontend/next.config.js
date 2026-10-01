/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  outputFileTracingRoot: __dirname,
  allowedDevOrigins: ["127.0.0.1", "localhost"],
  async rewrites() {
    // Proxy API calls to the Go backend during development.
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.DENDRITE_API_ORIGIN || "http://127.0.0.1:8080"}/api/:path*`,
      },
    ];
  },
};

module.exports = nextConfig;
