@@ Line 1 (prev 1) @@
  # Example config & installer snippets
  
  This file contains example package.json, Cargo.toml, tsconfig.json and simple installer files (Dockerfile, docker-compose.yml, Makefile) you can adapt for the project.
  
  ------------------------------------------------------------
  1) package.json (frontend / admin tooling)
  ------------------------------------------------------------
  /* @tweakable [Frontend Node engine major version to require for builds] */
  const NODE_ENGINE = ">=16"
  
  /* @tweakable [Project frontend package version] */
  const FRONTEND_VERSION = "0.1.0"
  
  {
    "name": "osm-sim-frontend",
    "version": "0.1.0",
    "private": true,
    "engines": {
      "node": ">=16"
    },
    "scripts": {
      "start": "serve -s . -l 8080",
      "build": "echo \"No build step (ES modules)\"",
      "lint": "eslint . --ext .js",
      "format": "prettier --write ."
    },
    "dependencies": {
      "leaflet": "^1.9.4"
    },
    "devDependencies": {
      "serve": "^14.0.1",
      "eslint": "^8.0.0",
      "prettier": "^2.0.0"
    }
  }
  
  ------------------------------------------------------------
  2) services/rust/Cargo.toml (example)
  ------------------------------------------------------------
  /* @tweakable [Rust edition used by services/rust crate] */
  const RUST_EDITION = "2021"
  
  /* @tweakable [Rust service crate version] */
  const RUST_SERVICE_VERSION = "0.1.0"
  
  [package]
  name = "gpx-parser-service"
  version = "0.1.0"
  edition = "2021"
  authors = ["Dev <dev@example.com>"]
  description = "Lightweight GPX parsing & summary service (example)"
  license = "MIT"
  
  [dependencies]
  serde = { version = "1.0", features = ["derive"] }
  serde_json = "1.0"
  warp = "0.3"
  tokio = { version = "1", features = ["full"] }
  gpx = "0.10"
  
  [profile.release]
  opt-level = 3
  
  ------------------------------------------------------------
  3) tsconfig.json (for optional TS tooling)
  ------------------------------------------------------------
  /* @tweakable [Allow JS interop in TS projects] */
  const TS_ALLOW_JS = true
  
  {
    "compilerOptions": {
      "target": "ES2020",
      "module": "ES2020",
      "moduleResolution": "bundler",
      "lib": ["ES2020", "DOM"],
      "strict": true,
      "esModuleInterop": true,
      "allowJs": true,
      "checkJs": false,
      "skipLibCheck": true,
      "forceConsistentCasingInFileNames": true,
      "resolveJsonModule": true,
      "isolatedModules": true,
      "noEmit": true
    },
    "include": ["*.js", "*.ts", "**/*.js", "**/*.ts"],
    "exclude": ["node_modules", "dist"]
  }
  
  ------------------------------------------------------------
  4) Dockerfile (simple multi-service frontend / static server)
  ------------------------------------------------------------
  /* @tweakable [Static server port exposed by Dockerfile] */
  const DOCKER_PORT = 8080
  
  # Use a tiny static server image for frontend ES modules
  FROM node:18-alpine AS builder
  WORKDIR /app
  COPY . .
  RUN npm install --only=prod serve
  
  FROM node:18-alpine
  WORKDIR /app
  COPY --from=builder /app /app
  EXPOSE 8080
  CMD ["npx", "serve", "-s", ".", "-l", "8080"]
  
  ------------------------------------------------------------
  5) docker-compose.yml (compose to run route-engine    frontend    rust)
  ------------------------------------------------------------
  /* @tweakable [Go route-engine listening port] */
  const ROUTE_ENGINE_PORT = 8081
  
  version: "3.8"
  services:
    frontend:
      build: .
      image: osm-sim-frontend:latest
      ports:
        - "8080:8080"
      volumes:
        - ./:/app
      command: ["npx","serve","-s",".","-l","8080"]
    route-engine:
      build: ./services/go
      image: route-engine:latest
      ports:
        - "8081:8081"
      environment:
        - MAX_DRIVERS=5000
    gpx-service:
      build: ./services/rust
      image: gpx-parser-service:latest
      ports:
        - "8082:8082"
  
  ------------------------------------------------------------
  6) Makefile (convenience tasks)
  ------------------------------------------------------------
  /* @tweakable [Default docker-compose profile to use for quick dev] */
  const MAKE_COMPOSE_PROFILE = "dev"
  
  .PHONY: up down build lint
  
  up:
  	docker-compose up --build
  
  down:
  	docker-compose down
  
  build:
  	docker-compose build
  
  lint:
  	# Frontend lint
  	npx eslint .
  
  clean:
  	rm -rf dist
  
  ------------------------------------------------------------
  Notes
  - These examples are intentionally minimal and meant as starting templates.
  - Adjust versions, extra dependencies and build steps to match your CI/CD preferences.
  - @tweakable annotations at top of each section let you quickly tune engine, edition, ports and profile values referenced in the snippets.




  - @tweakable annotations at top of each section let you quickly tune engine, edition, ports and profile values referenced in the snippets.
  =======
  # Working demo: expanded configs & runnable playbook
  
  This file now contains concrete, ready-to-run demo assets and instructions to launch the project locally (frontend, Go route-engine, Rust GPX service) using Docker Compose or locally via Make targets. Each key configurable value exposed for tuning uses a @tweakable JSDoc-style annotation so you can adjust behavior without editing service code.
  
  Quick overview
  - docker-compose.yml will spin up: frontend static server (serve), route-engine (Go) and gpx-service (Rust).
  - .env supplies tunable values fed into the Go route-engine.
  - Makefile provides convenience commands.
  - Minimal package.json and services/rust/Cargo.toml are included as runnable examples.
  
  Tweakables (edit here or override via .env)
  - MAX_DRIVERS: maximum number of managed drivers for the route engine
  - INITIAL_DRIVER_COUNT: number of synthetic drivers generated at startup
  - ROUTE_ENGINE_PORT: port exposed by the Go engine
  - FRONTEND_PORT: port used to serve frontend static files
  
  /* @tweakable [Max number of drivers route-engine will manage (for demo)] */
  const MAX_DRIVERS = 5000;
  
  /* @tweakable [Number of synthetic drivers to create at startup for demo] */
  const INITIAL_DRIVER_COUNT = 100;
  
  /* @tweakable [Port to expose the Go route engine on the host] */
  const ROUTE_ENGINE_PORT = 8081;
  
  /* @tweakable [Port to expose the frontend static server on the host] */
  const FRONTEND_PORT = 8080;
  
  Files included below are templates you can write into your repo to run the demo.
  
  1) .env (create at repo root)
3.18
  1
@@ Line 1 (prev 1) @@
  7) services/rust/Dockerfile
drivers.json
  1
@@ Line 1 (prev 1) @@
  2) docker-compose.yml
0.3
  1
@@ Line 1 (prev 1) @@
  6) services/go/Dockerfile (simple Dockerfile for the route-engine)
0.1.0
  1
@@ Line 1 (prev 1) @@
  5) services/rust/Cargo.toml (minimal runnable example)
3.8
  1
@@ Line 1 (prev 1) @@
  3) Makefile (repo root)
main.go
  1
@@ Line 1 (prev 1) @@
  4) package.json (repo root frontend helper)
