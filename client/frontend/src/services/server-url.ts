const uploadEndpoint = /\/v2\/collector\/samples$/;

/** GUI 只展示 Server 基地址，隐藏 Agent 实际使用的上传端点。 */
export function serverBase(url: string): string {
  return url.replace(uploadEndpoint, "");
}

export function isV2Server(url: string): boolean {
  return uploadEndpoint.test(url);
}
