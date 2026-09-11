export const poolFixture = () => ({
 outbounds:[{tag:'p1',protocol:{type:'vless',server:'proxy.example',port:443,id:'550e8400-e29b-41d4-a716-446655440000',tls:{server_name:'tls.example',alpn:['h2'],client_fingerprint:'chrome'},ws:{path:'/ws',headers:{Host:'host.example','X-Custom':'keep'}},mux_concurrency:8}},{tag:'p2',protocol:{type:'shadowsocks',server:'ss.example',port:8388,cipher:'aes-128-gcm',password:' literal secret '}}],
 outbound_groups:[{tag:'auto',type:'url_test',outbounds:['p1','p2'],interval_seconds:300,tolerance_ms:50},{tag:'chain',type:'relay',proxies:['p2','p1']},{tag:'pick',type:'selector',outbounds:['auto','chain'],default:'auto'}],target:'pick'
})
