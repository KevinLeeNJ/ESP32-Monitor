const connectionStates: Record<string, string> = {
  connected: "已连接", disconnected: "未连接", connecting: "连接中", pending: "等待确认",
};

const connectionTypes: Record<string, string> = { lan: "局域网", serial: "串口" };

export const connectionLabel = (state?: string) => connectionStates[state ?? ""] ?? (state || "空闲");

export const connectionTypeLabel = (type: string) => connectionTypes[type] ?? type;
