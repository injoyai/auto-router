import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Input, Button, Alert, Typography } from 'antd'
import { ThunderboltFilled } from '@ant-design/icons'
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
      setError('登录失败，请检查密码')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="aurora-login">
      <Card className="aurora-login-card" style={{ width: 400, padding: '40px 32px' }}>
        <div style={{ marginBottom: 32, textAlign: 'center' }}>
          <div style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 56,
            height: 56,
            borderRadius: 14,
            background: '#3a6b4d',
            color: '#fff',
            fontSize: 26,
            marginBottom: 18,
          }}>
            <ThunderboltFilled />
          </div>
          <Title level={3} style={{ marginBottom: 4, fontFamily: "'Bricolage Grotesque', sans-serif", fontWeight: 700, letterSpacing: '-0.02em' }}>
            Auto Router
          </Title>
          <p style={{ color: '#9a9078', fontSize: 13, fontFamily: "'JetBrains Mono', monospace", margin: 0, letterSpacing: '0.08em' }}>
            AI GATEWAY CONSOLE
          </p>
        </div>
        {error && <Alert type="error" message={error} style={{ marginBottom: 16, borderRadius: 8 }} showIcon />}
        <Input.Password
          placeholder="管理后台密码"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          onPressEnter={handleSubmit}
          style={{ marginBottom: 16, height: 44, borderRadius: 8 }}
          size="large"
        />
        <Button type="primary" block size="large" loading={loading} onClick={handleSubmit} style={{ height: 44, borderRadius: 8, fontWeight: 600, fontSize: 15 }}>
          登录
        </Button>
      </Card>
    </div>
  )
}
