import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { createTheme, ThemeProvider } from '@mui/material'
import Home from './pages/Home.tsx'
import Login from './pages/Login.tsx'
import Workbench from './pages/workbench/Workbench.tsx'
import SealedGate from './session/SealedGate.tsx'
import RequireSession from './session/RequireSession.tsx'

// Follows the OS light/dark preference, as index.css does
const theme = createTheme({ colorSchemes: { light: true, dark: true } })

function App() {
  return (
    <ThemeProvider theme={theme}>
      <SealedGate>
        <BrowserRouter>
          <Routes>
            <Route path="/" element={<Navigate to="/home" replace />} />
            <Route path="/home" element={<Home />} />
            <Route path="/login" element={<Login />} />
            <Route path="/setup" element={<Login setup />} />
            <Route
              path="/workbench"
              element={
                <RequireSession>
                  <Workbench />
                </RequireSession>
              }
            />
          </Routes>
        </BrowserRouter>
      </SealedGate>
    </ThemeProvider>
  )
}

export default App
