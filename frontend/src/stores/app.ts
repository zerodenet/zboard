import { defineStore } from 'pinia'
import { clearAuthToken, fetchPublicSystemConfigs, getAuthToken, getSetupStatus, setAuthToken, me as fetchMe, type SetupStatus } from '../api/client'
import { DEFAULT_SYSTEM_TIME_ZONE, setDisplayTimeZone } from '../utils/timeZone'

export const useAppStore = defineStore('app', {
  state: () => ({
    token: getAuthToken(),
    installation: null as SetupStatus | null,
    setupChecked: false,
    systemTimeZone: DEFAULT_SYSTEM_TIME_ZONE,
    user: {
      id: 0,
	  email: '',
      isAdmin: false,
    }
  }),
  getters: {
    isAuthenticated: (state) => Boolean(state.token),
    isAdmin: (state) => Boolean(state.user?.isAdmin),
    isInstalled: (state) => Boolean(state.installation?.installed),
    siteName: (state) => state.installation?.site_name || 'zboard'
  },
  actions: {
    setSystemTimeZone(value: unknown) {
      this.systemTimeZone = setDisplayTimeZone(value)
      return this.systemTimeZone
    },
    async loadSystemTimeZone() {
      try {
        const configs = await fetchPublicSystemConfigs()
        const config = configs.find(item => item.config_key === 'system_timezone')
        return this.setSystemTimeZone(config?.value)
      } catch (_) {
        return this.setSystemTimeZone(DEFAULT_SYSTEM_TIME_ZONE)
      }
    },
    async loadSetupStatus(force = false) {
      if (this.setupChecked && !force) {
        return this.installation
      }
      this.installation = await getSetupStatus()
      this.setupChecked = true
      if (this.installation?.installed) await this.loadSystemTimeZone()
      else this.setSystemTimeZone(DEFAULT_SYSTEM_TIME_ZONE)
      return this.installation
    },
    completeSetup(result: any) {
      this.installation = {
        installed: true,
        site_name: result.site_name || 'zboard',
        version: ''
      }
      this.setupChecked = true
      this.setSystemTimeZone(DEFAULT_SYSTEM_TIME_ZONE)
      if (result.auth?.token) {
        this.setToken(result.auth.token)
      }
      if (result.user) {
        this.setUser(result.user)
      }
    },
    setToken(token: string) {
      this.token = token
      setAuthToken(token)
    },
    setUser(user: any) {
      this.user = {
        id: user.id,
		email: user.email,
        isAdmin: Boolean(user.is_admin ?? user.isAdmin)
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
	  this.user = { id: 0, email: '', isAdmin: false }
      clearAuthToken()
    }
  }
})
