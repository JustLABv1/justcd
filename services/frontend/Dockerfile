ARG NODE_IMAGE=node:24-alpine3.24

FROM ${NODE_IMAGE} AS dependencies
WORKDIR /app
RUN npm install --global pnpm@12.4.2
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

FROM dependencies AS build
COPY . .
RUN pnpm build

FROM ${NODE_IMAGE} AS runtime
# The standalone server only needs Node, not package managers or build tooling.
RUN rm -rf /usr/local/lib/node_modules/npm /opt/yarn-* \
    && rm -f /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/yarn /usr/local/bin/yarnpkg
ENTRYPOINT ["node"]
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0 HOME=/tmp
WORKDIR /app
COPY --from=build --chown=10001:10001 /app/.next/standalone ./
COPY --from=build --chown=10001:10001 /app/.next/static ./.next/static
COPY --from=build --chown=10001:10001 /app/public ./public
USER 10001:10001
EXPOSE 3000
CMD ["server.js"]
