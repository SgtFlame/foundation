import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import Home from './pages/Home.tsx'
import Login from './pages/Login.tsx'
import Workbench from './pages/workbench/Workbench.tsx'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Navigate to="/home" replace />} />
        <Route path="/home" element={<Home />} />
        <Route path="/login" element={<Login />} />
        <Route path="/workbench" element={<Workbench />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
