import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout as AntLayout, Menu, Button } from 'antd'
import {
  DashboardOutlined,
  AppstoreOutlined,
  SettingOutlined,
  FileTextOutlined,
  LogoutOutlined,
} from '@ant-design/icons'

const { Sider, Content } = AntLayout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/sources', icon: <AppstoreOutlined />, label: '模型管理' },
  { key: '/routing', icon: <SettingOutlined />, label: '路由配置' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
]

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = location.pathname === '/' ? '/' : '/' + location.pathname.split('/').filter(Boolean)[0]

  const handleLogout = () => {
    localStorage.removeItem('admin_jwt')
    navigate('/login')
  }

  return (
    <AntLayout style={{ minHeight: '100vh' }}>
      <Sider width={220} theme="light" style={{ borderRight: '1px solid #f0f0f0', position: 'relative' }}>
        <div style={{ padding: '16px', fontWeight: 700, fontSize: 16, color: '#08979c' }}>
          Auto Router
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          style={{ borderRight: 0 }}
        />
        <div style={{ position: 'absolute', bottom: 0, width: '100%', padding: '12px 16px' }}>
          <Button icon={<LogoutOutlined />} block onClick={handleLogout}>
            退出登录
          </Button>
        </div>
      </Sider>
      <AntLayout>
        <Content style={{ padding: 24 }}>
          <Outlet />
        </Content>
      </AntLayout>
    </AntLayout>
  )
}
