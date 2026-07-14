import { Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout } from './components/Layout/AppLayout'
import { LoginPage } from './pages/LoginPage'
import { MailPage } from './pages/MailPage'
import { SettingsPage } from './pages/SettingsPage'

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/login" replace />} />
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AppLayout />}>
        <Route path="/mail" element={<MailPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/login" replace />} />
    </Routes>
  )
}
