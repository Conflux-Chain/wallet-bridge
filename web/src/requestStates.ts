export const requestStates = {
  pending: '待审批', succeeded: '已同意连接', awaiting_wallet: '等待钱包',
  uncertain: '结果未确定', sent: '已发送', rejected: '用户已拒绝', canceled: '已取消', expired: '已到期',
} as const;
