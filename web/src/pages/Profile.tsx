import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../lib/api'
import type { PlatformSettings, User } from '../lib/types'
import { Badge, Button, Card, Field, Input, useToast } from '../components/ui'
import { KeyIcon, MailIcon, ShieldIcon } from '../components/icons'

export default function Profile() {
  const toast = useToast()
  const [user, setUser] = useState<User | null>(null)
  const [settings, setSettings] = useState<PlatformSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // 修改密码
  const [oldPass, setOldPass] = useState('')
  const [newPass, setNewPass] = useState('')
  const [confirmPass, setConfirmPass] = useState('')
  const [pwBusy, setPwBusy] = useState(false)
  const [pwError, setPwError] = useState('')

  // 绑定邮箱
  const [email, setEmail] = useState('')
  const [emailBusy, setEmailBusy] = useState(false)
  const [emailError, setEmailError] = useState('')
  const [emailInfo, setEmailInfo] = useState('')
  const [code, setCode] = useState('')
  const [codeBusy, setCodeBusy] = useState(false)
  const [codeError, setCodeError] = useState('')

  const load = useCallback(async () => {
    try {
      const [u, s] = await Promise.all([
        api.profile(),
        api.settings().catch(() => null),
      ])
      setUser(u)
      setSettings(s)
      setError('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const submitPassword = async () => {
    setPwError('')
    if (newPass.length < 6) {
      setPwError('新密码至少 6 位')
      return
    }
    if (newPass !== confirmPass) {
      setPwError('两次输入的新密码不一致')
      return
    }
    setPwBusy(true)
    try {
      await api.changeLoginPassword(oldPass, newPass)
      toast('登录密码已修改')
      setOldPass('')
      setNewPass('')
      setConfirmPass('')
    } catch (err) {
      setPwError(err instanceof ApiError ? err.message : '修改失败')
    } finally {
      setPwBusy(false)
    }
  }

  const submitEmail = async () => {
    setEmailError('')
    setEmailInfo('')
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      setEmailError('请输入有效的邮箱地址')
      return
    }
    setEmailBusy(true)
    try {
      const res = await api.bindEmail(email.trim())
      setUser((u) => (u ? { ...u, email: res.email, email_verified: res.email_verified } : u))
      if (res.code_sent) {
        setEmailInfo(res.message ?? '验证码已发送到邮箱，请查收后完成验证')
      } else {
        setEmailInfo('邮箱已绑定')
        toast('邮箱绑定成功')
      }
    } catch (err) {
      setEmailError(err instanceof ApiError ? err.message : '绑定失败')
    } finally {
      setEmailBusy(false)
    }
  }

  const submitCode = async () => {
    setCodeError('')
    if (!code.trim()) {
      setCodeError('请输入验证码')
      return
    }
    setCodeBusy(true)
    try {
      const res = await api.verifyEmail(code.trim())
      setUser((u) => (u ? { ...u, email: res.email, email_verified: true } : u))
      setCode('')
      setEmailInfo('邮箱验证通过')
      toast('邮箱已验证')
    } catch (err) {
      setCodeError(err instanceof ApiError ? err.message : '验证失败')
    } finally {
      setCodeBusy(false)
    }
  }

  if (loading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-blue-600" />
      </div>
    )
  }
  if (error || !user) {
    return <Card className="p-8 text-center text-sm text-red-600">{error || '加载失败'}</Card>
  }

  const isAdmin = user.role === 'admin'
  const verifyEnabled = settings?.email_verify_enabled === '1'

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-lg font-semibold text-slate-900">
          个人信息 <span className="text-sm font-normal text-slate-400">Profile</span>
        </h1>
        <p className="mt-0.5 text-sm text-slate-500">管理你的登录密码与邮箱绑定</p>
      </div>

      {/* 账号概览 */}
      <Card>
        <div className="flex flex-wrap items-center gap-4 border-b border-slate-100 px-5 py-4">
          <div className="flex h-12 w-12 items-center justify-center rounded-full border-2 border-black bg-lavender font-mono text-base font-bold text-black">
            {user.username.slice(0, 2).toUpperCase()}
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="text-base font-bold text-slate-900">{user.username}</span>
              <Badge color={isAdmin ? 'purple' : 'blue'}>{isAdmin ? '管理员' : '普通用户'}</Badge>
            </div>
            <div className="mt-0.5 text-sm text-slate-500">
              注册于 {new Date(user.created_at).toLocaleDateString()} · 余额 ¥{(user.balance_cents / 100).toFixed(2)}
            </div>
          </div>
        </div>

        <div className="grid gap-6 p-5 md:grid-cols-2">
          {/* 登录密码 */}
          <div>
            <div className="mb-3 flex items-center gap-2">
              <KeyIcon className="h-4 w-4 text-slate-400" />
              <span className="text-sm font-semibold text-slate-800">修改登录密码</span>
            </div>
            <div className="space-y-3">
              <Field label="当前密码">
                <Input type="password" value={oldPass} onChange={(e) => setOldPass(e.target.value)} placeholder="输入当前密码" autoComplete="current-password" />
              </Field>
              <Field label="新密码" hint="至少 6 位">
                <Input type="password" value={newPass} onChange={(e) => setNewPass(e.target.value)} placeholder="输入新密码" autoComplete="new-password" />
              </Field>
              <Field label="确认新密码">
                <Input type="password" value={confirmPass} onChange={(e) => setConfirmPass(e.target.value)} placeholder="再次输入新密码" autoComplete="new-password" />
              </Field>
              {pwError && <p className="text-sm text-red-600">{pwError}</p>}
              <Button onClick={submitPassword} disabled={pwBusy || !oldPass || !newPass || !confirmPass}>
                {pwBusy ? '保存中…' : '保存新密码'}
              </Button>
            </div>
          </div>

          {/* 邮箱绑定 */}
          <div>
            <div className="mb-3 flex items-center gap-2">
              <MailIcon className="h-4 w-4 text-slate-400" />
              <span className="text-sm font-semibold text-slate-800">绑定邮箱</span>
            </div>
            <div className="space-y-3">
              <Field
                label="邮箱地址"
                hint={user.email ? `当前绑定：${user.email}` : '用于找回密码与接收通知'}
              >
                <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />
              </Field>
              {user.email && (
                <div className="flex items-center gap-2 text-sm">
                  <span className="text-slate-500">验证状态：</span>
                  {user.email_verified ? (
                    <Badge color="green">已验证</Badge>
                  ) : (
                    <Badge color="orange" dot>待验证</Badge>
                  )}
                </div>
              )}
              {emailInfo && <p className="text-sm text-green-600">{emailInfo}</p>}
              {emailError && <p className="text-sm text-red-600">{emailError}</p>}
              <Button onClick={submitEmail} disabled={emailBusy || !email.trim()}>
                {emailBusy ? '保存中…' : user.email ? '更换邮箱' : '绑定邮箱'}
              </Button>

              {verifyEnabled && user.email && !user.email_verified && (
                <div className="mt-4 space-y-3 rounded-xl border-2 border-slate-100 bg-slate-50 p-4">
                  <p className="text-sm font-medium text-slate-700">输入邮箱中收到的验证码完成验证</p>
                  <div className="flex gap-2">
                    <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="6 位验证码" maxLength={6} className="font-mono" />
                    <Button variant="sky" onClick={submitCode} disabled={codeBusy || !code.trim()}>
                      {codeBusy ? '验证中…' : '验证'}
                    </Button>
                  </div>
                  {codeError && <p className="text-sm text-red-600">{codeError}</p>}
                </div>
              )}
              {!verifyEnabled && (
                <p className="text-xs text-slate-400">平台当前未强制邮箱验证，绑定后即视为已验证。</p>
              )}
            </div>
          </div>
        </div>
      </Card>

      {/* 管理员：发件邮箱配置（只读预览，完整配置在管理后台） */}
      {isAdmin && (
        <Card>
          <div className="flex items-center gap-2 border-b border-slate-100 px-5 py-3">
            <ShieldIcon className="h-4 w-4 text-slate-400" />
            <span className="text-sm font-semibold text-slate-800">平台设置（管理员）</span>
          </div>
          <div className="space-y-2 px-5 py-4 text-sm text-slate-600">
            <p>
              SMTP 发件服务器：<span className="font-mono">{settings?.smtp_host ? `${settings.smtp_host}:${settings.smtp_port || '587'}` : '未配置'}</span>
              {settings?.smtp_from && <> · 发件人 <span className="font-mono">{settings.smtp_from}</span></>}
            </p>
            <p>
              邮箱验证：<Badge color={verifyEnabled ? 'green' : 'slate'}>{verifyEnabled ? '已开启' : '已关闭'}</Badge>
              {'　'}
              Cloudflare 人机验证：<Badge color={settings?.cloudflare_captcha_enabled === '1' ? 'green' : 'slate'}>
                {settings?.cloudflare_captcha_enabled === '1' ? '已开启（预留）' : '关闭（预留）'}
              </Badge>
            </p>
          </div>
        </Card>
      )}
    </div>
  )
}
