import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

// Extension points declare themselves in their owner's points.ts, and features
// contribute in their contributions.ts (documentation/plans/Contributors.md)
import.meta.glob(['./**/points.ts', './**/contributions.ts'], { eager: true })

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
