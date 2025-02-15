/** @type {import('next').NextConfig} */
const nextConfig = {
  output: process.env.NODE_ENV === 'development' ? undefined : 'export',
  basePath: process.env.NODE_ENV === 'development' ? '' : '/admin',
  trailingSlash: true,
  eslint: {
    ignoreDuringBuilds: true,
  },
  // 开发环境配置代理
  async rewrites() {
    if (process.env.NODE_ENV !== 'development') {
      return []
    }
    return [
      {
        source: '/api/:path*',
        destination: 'http://localhost:3336/admin/api/:path*',
      },
    ]
  },
}

module.exports = nextConfig 