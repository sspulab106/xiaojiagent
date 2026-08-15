import { Navigate, Route, Routes } from 'react-router-dom'
import { getToken } from './lib/api'
import { ToastProvider } from './components/ui'
import Layout from './components/Layout'
import Login from './pages/Login'
import Landing from './pages/Landing'
import Dashboard from './pages/Dashboard'
import Instances from './pages/Instances'
import InstanceDetail from './pages/InstanceDetail'
import Terminal from './pages/Terminal'
import Hosting from './pages/Hosting'
import Recharge from './pages/Recharge'
import Market from './pages/Market'
import Support from './pages/Support'
import Profile from './pages/Profile'
import Placeholder from './pages/Placeholder'
import Admin from './pages/Admin'

function RequireAuth({ children }: { children: React.ReactNode }) {
  return getToken() ? <>{children}</> : <Navigate to="/login" replace />
}

export default function App() {
  return (
    <ToastProvider>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/landing" element={<Landing />} />
        <Route
          element={
            <RequireAuth>
              <Layout />
            </RequireAuth>
          }
        >
          <Route path="/" element={<Dashboard />} />
          <Route path="/instances" element={<Instances />} />
          <Route path="/instances/:id" element={<InstanceDetail />} />
          <Route path="/terminal/:id" element={<Terminal />} />
          <Route path="/hosting" element={<Hosting />} />
          <Route path="/recharge" element={<Recharge />} />
          <Route path="/ip" element={<Placeholder title="专属IP" en="Dedicated IP" />} />
          <Route path="/market" element={<Market />} />
          <Route path="/support" element={<Support />} />
          <Route path="/profile" element={<Profile />} />
          <Route path="/admin" element={<Admin />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </ToastProvider>
  )
}
