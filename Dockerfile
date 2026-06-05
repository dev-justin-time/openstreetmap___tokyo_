FROM node:20-alpine
RUN npm install -g serve
WORKDIR /app
COPY package.json ./
RUN npm install
COPY . .
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=2 \
  CMD wget -qO- http://127.0.0.1:8080/ || exit 1
USER nobody
ENTRYPOINT ["npx", "serve", "-s", ".", "-l", "8080", "--no-clipboard"]
