// Monaco bundled with the app rather than loaded from a CDN. y-monaco imports
// monaco-editor's editor.api module, the same instance re-exported here.

import { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'

// Markdown has no language worker; every label gets the core editor worker
self.MonacoEnvironment = { getWorker: () => new EditorWorker() }

loader.config({ monaco })
