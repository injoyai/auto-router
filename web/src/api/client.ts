import axios from 'axios'

const apiClient = axios.create({ baseURL: '' })

apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('admin_jwt')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

apiClient.interceptors.response.use(
  (resp) => resp,
  (err) => {
    if (err.response?.status === 401 && !err.config?.url?.includes('/admin/login')) {
      localStorage.removeItem('admin_jwt')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

export default apiClient
