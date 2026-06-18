import { defineConfig } from 'vitest/config'

// Pure-logic unit tests (protocol getters/guards, subtitle reducer).
// Single non-isolated thread keeps the worker pool minimal and portable
// across constrained CI/sandbox environments.
export default defineConfig({
  test: {
    environment: 'node',
    pool: 'threads',
    poolOptions: {
      threads: {
        singleThread: true,
        isolate: false,
      },
    },
  },
})
