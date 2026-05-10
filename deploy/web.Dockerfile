FROM node:25-alpine AS builder

WORKDIR /app

COPY apps/web/package*.json ./
RUN npm install

COPY apps/web ./
RUN npm run build

FROM nginx:1.27-alpine

COPY deploy/web-nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 1002

