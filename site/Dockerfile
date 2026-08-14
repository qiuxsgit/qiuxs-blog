FROM node:20.19.4-bookworm-slim
WORKDIR /workspace/site
COPY site/package*.json ./
RUN npm ci
COPY site ./
COPY contracts ./../contracts
RUN npm run build
