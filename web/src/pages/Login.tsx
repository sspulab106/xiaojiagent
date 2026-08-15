import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, ApiError, setToken } from '../lib/api'
import { Button, Field, Input, Spinner } from '../components/ui'
import { FishIcon } from '../components/icons'

export default function Login() {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const navigate = useNavigate()

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = mode === 'login' ? await api.login(username, password) : await api.register(username, password)
      setToken(res.token)
      navigate('/')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '请求失败，请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-14 w-14 animate-pop-in items-center justify-center rounded-2xl border-2 border-black bg-lavender text-black shadow-hard">
            <FishIcon className="h-7 w-7" />
          </div>
          <h1 className="text-2xl font-extrabold text-ink">独角鲸云</h1>
          <p className="mt-1 font-mono text-xs font-medium text-muted">Narwhal Cloud · 容器合租 / NAT VPS 管理面板</p>
        </div>

        <form onSubmit={submit} className="rounded-2xl border-[2.5px] border-black bg-white p-6 shadow-hard-lg">
          <div className="space-y-4">
            <Field label="用户名">
              <Input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="请输入用户名"
                autoComplete="username"
                required
              />
            </Field>
            <Field label="密码">
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="请输入密码"
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                required
              />
            </Field>
          </div>

          {error && <p className="mt-3 text-sm text-red-600">{error}</p>}

          <Button type="submit" size="lg" className="mt-5 w-full" disabled={loading}>
            {loading ? <Spinner /> : mode === 'login' ? '登录' : '注册'}
          </Button>

          <p className="mt-4 text-center text-sm text-body">
            {mode === 'login' ? (
              <>
                还没有账号？
                <button type="button" onClick={() => setMode('register')} className="font-medium text-brand hover:underline">
                  立即注册
                </button>
              </>
            ) : (
              <>
                已有账号？
                <button type="button" onClick={() => setMode('login')} className="font-medium text-brand hover:underline">
                  返回登录
                </button>
              </>
            )}
          </p>
        </form>

        <p className="mt-6 text-center">
          <Link to="/landing" className="rounded-xl border-2 border-black bg-white px-3 py-1.5 font-mono text-xs font-bold text-ink shadow-hard-sm transition-all duration-100 hover:-translate-x-[1px] hover:-translate-y-[1px] hover:shadow-hard active:translate-x-[2px] active:translate-y-[2px] active:shadow-none">
            ← 返回官网首页
          </Link>
        </p>
      </div>
    </div>
  )
}
