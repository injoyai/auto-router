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
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#f5f5f5',
      }}
    >
      <Card style={{ width: 400 }}>
        <Title level={4} style={{ textAlign: 'center', marginBottom: 24 }}>
          Auto Router 管理后台
        </Title>
        {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} showIcon />}
        <Input.Password
          placeholder="请输入 Admin Token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          onPressEnter={handleSubmit}
          style={{ marginBottom: 16 }}
          size="large"
        />
        <Button type="primary" block size="large" loading={loading} onClick={handleSubmit}>
          登录
        </Button>
      </Card>
    </div>
  )
}
