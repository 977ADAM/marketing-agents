# Образ фронта: SvelteKit (adapter-node) — отдаёт приложение и проксирует
# /api/* на Go-API через endpoint-роут src/routes/api/[...path]/+server.ts.
#
# Контекст сборки — каталог frontend/ (docker build ./frontend).

# 1) сборка
FROM node:22-alpine AS build
WORKDIR /app
# Базовый путь приложения: '/' для standalone, '/marketing/' под interpool.
ARG PUBLIC_BASE=/
ENV PUBLIC_BASE=$PUBLIC_BASE
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# 2) рантайм: adapter-node кладёт всё нужное в build/ (зависимости вкомпилированы)
FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production \
    PORT=8080
COPY --from=build /app/build ./build
COPY --from=build /app/package.json ./package.json
USER node
EXPOSE 8080
ENTRYPOINT ["node", "build"]
