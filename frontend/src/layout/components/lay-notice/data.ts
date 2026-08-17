export interface ListItem {
  id?: number;
  avatar: string;
  title: string;
  datetime: string;
  type: string;
  description: string;
  status?: "primary" | "success" | "warning" | "info" | "danger";
  extra?: string;
  read?: boolean;
}

export interface TabItem {
  key: string;
  name: string;
  list: ListItem[];
  emptyText: string;
}

export interface Message {
  id: number;
  user_id: number;
  username: string;
  title: string;
  content: string;
  type: string;
  status: string;
  read: boolean;
  created_at: string;
}

export const noticesData: TabItem[] = [
  // 暂时隐藏通知
  // {
  //   key: "1",
  //   name: "通知",
  //   list: [],
  //   emptyText: "暂无通知"
  // },
  {
    key: "2",
    name: "消息",
    list: [],
    emptyText: "暂无消息"
  },
  {
    key: "3",
    name: "任务",
    list: [],
    emptyText: "暂无待办任务"
  }
];
