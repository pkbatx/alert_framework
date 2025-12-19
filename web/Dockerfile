# syntax=docker/dockerfile:1

FROM node:20-alpine AS builder
WORKDIR /app

COPY package.json ./
RUN corepack enable && pnpm install

COPY . .

ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8000
ARG NEXT_PUBLIC_BUILD_SHA=dev
ARG NEXT_PUBLIC_BUILD_TIME=unknown
ENV NEXT_PUBLIC_API_BASE_URL=${NEXT_PUBLIC_API_BASE_URL}
ENV NEXT_PUBLIC_BUILD_SHA=${NEXT_PUBLIC_BUILD_SHA}
ENV NEXT_PUBLIC_BUILD_TIME=${NEXT_PUBLIC_BUILD_TIME}

RUN pnpm build

FROM node:20-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production
RUN corepack enable

COPY --from=builder /app/.next /app/.next
COPY --from=builder /app/public /app/public
COPY --from=builder /app/package.json /app/package.json
COPY --from=builder /app/node_modules /app/node_modules

ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8000
ARG NEXT_PUBLIC_BUILD_SHA=dev
ARG NEXT_PUBLIC_BUILD_TIME=unknown
ENV NEXT_PUBLIC_API_BASE_URL=${NEXT_PUBLIC_API_BASE_URL}
ENV NEXT_PUBLIC_BUILD_SHA=${NEXT_PUBLIC_BUILD_SHA}
ENV NEXT_PUBLIC_BUILD_TIME=${NEXT_PUBLIC_BUILD_TIME}

EXPOSE 3000

CMD ["pnpm", "start"]
