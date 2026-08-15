// IP 探测辅助：用公共服务自动获取本机公网 IPv4 / IPv6 与地区。
// 全部为软依赖——任一失败返回空字符串，调用方优雅降级。

export interface IpInfo {
  ipv4: string
  ipv6: string
  region: string
}

export async function detectIpInfo(): Promise<IpInfo> {
  const [v4, v6, geo] = await Promise.allSettled([
    fetch('https://api.ipify.org?format=json').then((r) => r.json() as Promise<{ ip?: string }>),
    fetch('https://api6.ipify.org?format=json').then((r) => r.json() as Promise<{ ip?: string }>),
    fetch(
      'https://ip-api.com/json/?fields=status,message,query,city,regionName,countryCode&lang=zh-CN',
    ).then((r) => r.json() as Promise<{ status?: string; countryCode?: string; regionName?: string; city?: string }>),
  ])

  const ipv4 = v4.status === 'fulfilled' ? (v4.value.ip ?? '') : ''
  const ipv6 = v6.status === 'fulfilled' ? (v6.value.ip ?? '') : ''

  let region = ''
  if (geo.status === 'fulfilled' && geo.value.status === 'success') {
    const parts = [geo.value.countryCode, geo.value.regionName, geo.value.city].filter(
      (p, i, arr) => p && arr.indexOf(p) === i,
    )
    region = parts.join(' · ')
  }

  return { ipv4, ipv6, region }
}
