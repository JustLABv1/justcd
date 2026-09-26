FROM node:24-bookworm-slim AS dependencies
WORKDIR /app
RUN npm install --global pnpm@12.4.2
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

FROM dependencies AS build
COPY . .
RUN pnpm build

FROM gcr.io/distroless/nodejs24-debian13:nonroot AS runtime
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0 HOME=/tmp
WORKDIR /app
COPY --from=build --chown=10001:10001 /app/.next/standalone ./
COPY --from=build --chown=10001:10001 /app/.next/static ./.next/static
COPY --from=build --chown=10001:10001 /app/public ./public
USER 10001:10001
EXPOSE 3000
CMD ["server.js"]
