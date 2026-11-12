import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { createTheme, ThemeProvider } from '@mui/material'
import Home from '@foundation/common/pages/Home.tsx'
import Login from '@foundation/common/pages/Login.tsx'
import Workbench from '@foundation/common/pages/workbench/Workbench.tsx'
import SealedGate from '@foundation/common/session/SealedGate.tsx'
import RequireSession from '@foundation/common/session/RequireSession.tsx'

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
