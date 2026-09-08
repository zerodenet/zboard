(() => {
  const token = new URLSearchParams(location.hash.slice(1)).get('bridge_token')
  const requestID = 'welcome_context'
  const timer = setTimeout(() => { document.querySelector('#context').textContent = '宿主会话不可用，请重新加载。' }, 10000)
  function receive(event) {
    const m = event.data
    if (event.source !== parent || !m || m.source !== 'zboard-plugin-host' || m.bridge_token !== token || m.request_id !== requestID) return
    clearTimeout(timer)
    document.querySelector('#context').textContent = m.ok ? `当前范围：${({ public: '公开前台', account: '用户前台', admin: '管理后台' })[m.result.surface] || '未知'}` : '当前会话无权执行此操作。'
  }
  addEventListener('message', receive)
  parent.postMessage({ source: 'zboard-plugin-ui', bridge_token: token, type: 'context.load', request_id: requestID }, '*')
  addEventListener('pagehide', () => { clearTimeout(timer); removeEventListener('message', receive) }, { once: true })
})()
