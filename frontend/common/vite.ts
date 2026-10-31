import { createRequire } from 'node:module'
import { join, resolve } from 'node:path'
import type { Plugin } from 'vite'

// Backend routes, passed through to the server by the dev server
const routes = [
  'kb',
  'ai',
  'index',
  'extensions',
  'config',
  'usage',
  'logs',
  'conversation',
  'retrieval',
  'chronicle',
  'cognition',
  'identity',
  'authz',
  'secrets',
  'bootstrap',
]

// The Vite configuration every app built on common needs
export function foundation(backend = 'http://localhost:8080'): Plugin {
  return {
    name: 'foundation',
    config: (config) => ({
      // y-monaco and the editor must share one Monaco and one Yjs
      resolve: {
        dedupe: ['monaco-editor', 'yjs'],
        alias: [
          {
            // y-monaco imports this path, which monaco-editor 0.57's exports map no longer allows.
            // Resolved from the app, not this file, so an app outside this workspace gets its own Monaco
            find: /^monaco-editor\/esm\/vs\/editor\/editor\.api\.js$/,
            replacement: createRequire(join(resolve(config.root ?? '.'), 'package.json')).resolve(
              'monaco-editor/editor/editor.api.js',
            ),
          },
        ],
      },
      server: {
        proxy: Object.fromEntries(
          routes.map((route) => [`/api/v1/${route}`, { target: backend, ws: route === 'kb' }]),
        ),
      },
    }),
  }
}
