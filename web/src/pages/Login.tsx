import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Input, Button, Alert, Typography } from 'antd'
import { login } from '../api/auth'

const { Title } = Typography

export default function Login() {
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const navigate = useNavigate()

  const handleSubmit = async () => {
    if (!token.trim()) return
    setLoading(true)
    setError('')
    try {
      const res = await login(token.trim())
      localStorage.setItem('admin_jwt', res.token)
      navigate('/')
    } catch {
      setError('登录失败，请检查 Token')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="aurora-login-bg">
      <div className="aurora-grid-pattern" />
      <Card className="aurora-glass-card" style={{ width: 420, padding: '8px 12px' }}>
        <div style={{ textAlign: 'center', marginBottom: 28, marginTop: 8 }}>
          <div style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 56,
            height: 56,
            borderRadius: 16,
            background: 'linear-gradient(135deg, #13c2c2, #08979c)',
            marginBottom: 16,
            boxShadow: '0 8px 24px rgba(19, 194, 194, 0.3)',
          }}>
            <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5z" />
              <path d="M2 17l10 5 10-5" />
              <path d="M2 12l10 5 10-5" />
            </svg>
          </div>
          <Title level={3} style={{ marginBottom: 4, fontFamily: "'Outfit', sans-serif" }}>
            Auto Router
          </Title>
          <p style={{ color: '#64748b', fontSize: 13, fontFamily: "'JetBrains Mono', monospace", letterSpacing: '0.05em' }}>
            AI Model Gateway Console
          </p>
        </div>
        {error && <Alert type="error" message={error} style={{ marginBottom: 16, borderRadius: 8 }} showIcon />}
        <Input.Password
          placeholder="请输入 Admin Token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          onPressEnter={handleSubmit}
          style={{ marginBottom: 16, height: 46, borderRadius: 10 }}
          size="large"
        />
        <Button type="primary" block size="large" loading={loading} onClick={handleSubmit} style={{ height: 46, borderRadius: 10, fontWeight: 600, fontSize: 15 }}>
          登录
        </Button>
      </Card>
    </div>
  )
}
