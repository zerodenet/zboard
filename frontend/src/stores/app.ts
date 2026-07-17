import { defineStore } from 'pinia'
import { clearAuthToken, getAuthToken, setAuthToken, me as fetchMe } from '../api/client'

export const useAppStore = defineStore('app', {
  state: () => ({
    token: getAuthToken(),
    user: {
      id: 0,
      username: '',
      isAdmin: false,
    }
  }),
  getters: {
    isAuthenticated: (state) => Boolean(state.token),
    isAdmin: (state) => Boolean(state.user?.isAdmin)
  },
  actions: {
    setToken(token: string) {
      this.token = token
      setAuthToken(token)
    },
    setUser(user: any) {
      this.user = {
        id: user.id,
        username: user.username,
        isAdmin: user.isAdmin
      }
    },
    async loadMe() {
      if (!this.token) {
        return
      }
      try {
        const user = await fetchMe()
        if (user) {
          this.setUser(user)
        } else {
          this.clear()
        }
      } catch (_) {
        this.clear()
      }
    },
    clear() {
      this.token = ''
      this.user = { id: 0, username: '', isAdmin: false }
      clearAuthToken()
    }
  }
})
