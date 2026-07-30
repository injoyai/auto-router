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
    <div className="mono-login">
      <Card className="mono-login-card" style={{ width: 400, padding: '32px 28px' }}>
        <div style={{ marginBottom: 32 }}>
          <Title level={3} style={{ marginBottom: 4, fontFamily: "'Plus Jakarta Sans', sans-serif", fontWeight: 700 }}>
            Auto Router
          </Title>
          <p style={{ color: '#a3a3a3', fontSize: 13, fontFamily: "'JetBrains Mono', monospace", margin: 0 }}>
            AI Gateway Console
          </p>
        </div>
        {error && <Alert type="error" message={error} style={{ marginBottom: 16, borderRadius: 8 }} showIcon />}
        <Input.Password
          placeholder="Admin Token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          onPressEnter={handleSubmit}
          style={{ marginBottom: 16, height: 44, borderRadius: 8 }}
          size="large"
        />
        <Button type="primary" block size="large" loading={loading} onClick={handleSubmit} style={{ height: 44, borderRadius: 8, fontWeight: 600 }}>
          登录
        </Button>
      </Card>
    </div>
  )
}
