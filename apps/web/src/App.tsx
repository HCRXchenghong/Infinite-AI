import { Navigate, Route, Routes } from 'react-router-dom'
import { AdminApp } from './pages/AdminApp'
import { AdminLoginPage } from './pages/AdminLoginPage'
import { RedeemPage } from './pages/RedeemPage'
import { UserApp } from './pages/UserApp'

export default function App() {
  return (
    <Routes>
      <Route path="/admin/login" element={<AdminLoginPage />} />
      <Route path="/admin" element={<Navigate to="/admin/dashboard" replace />} />
      <Route path="/admin/*" element={<AdminApp />} />
      <Route path="/redeem" element={<RedeemPage />} />
      <Route path="/register" element={<UserApp />} />
      <Route path="/login" element={<UserApp />} />
      <Route path="*" element={<UserApp />} />
    </Routes>
  )
}
