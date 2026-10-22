# ============================================================
# 前端生产镜像：Nuxt 3 SSR（Node 运行时）
#
# 构建：docker build -f web/Dockerfile -t blog-web web
# 运行：node .output/server/index.mjs，监听 3000
#
# 为什么不用 nuxi generate：文章详情页依赖 SSR 才有 SEO 收益（TECH_DESIGN 15.1），
# 纯静态生成会退化成客户端取数。
# ============================================================

# ---------- 构建阶段 ----------
FROM node:22.11.0-alpine AS builder

WORKDIR /app

# 依赖层单独缓存：只改业务代码时不需要重装
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .
# SSR 阶段直连后端用的上游地址在运行时注入（NUXT_PUBLIC_API_UPSTREAM），
# 构建期不需要数据库或后端可用，构建产物与部署环境解耦
RUN npm run build

# ---------- 运行阶段 ----------
FROM node:22.11.0-alpine

WORKDIR /app
ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000

# 只带运行必需产物：server 与 public，源码与 devDependencies 不进镜像
COPY --from=builder /app/.output ./.output

# 非 root 运行（node 镜像自带 node 用户）
USER node

EXPOSE 3000

# 健康检查交给编排层（compose healthcheck），镜像内不重复定义
CMD ["node", ".output/server/index.mjs"]
