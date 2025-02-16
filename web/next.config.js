/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    return [
      {
        source: '/admin/api/:path*',
        destination: 'http://127.0.0.1:3336/admin/api/:path*',
      },
    ]
  },
}

module.exports = nextConfig 