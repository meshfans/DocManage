import { http } from "@/utils/http";

export interface Message {
  id: number;
  user_id: number;
  sender_id: number;
  title: string;
  content: string;
  type: string;
  status: string;
  read: boolean;
  created_at: string;
  updated_at: string;
  sender_name?: string;
}

export interface GetMessagesResponse {
  messages: Message[];
  total: number;
}

export interface GetUnreadCountResponse {
  count: number;
}

export interface CreateMessageRequest {
  user_id: number;
  sender_id?: number;
  title: string;
  content: string;
  type?: string;
}

export const getMessages = (limit?: number, offset?: number) => {
  return http.request<GetMessagesResponse>("get", "/api/messages/list", {
    params: { limit, offset }
  });
};

export const getUnreadCount = () => {
  return http.request<GetUnreadCountResponse>("get", "/api/messages/unread-count");
};

export const getMessage = (id: number) => {
  return http.request<Message>("get", `/api/messages/${id}`);
};

export const createMessage = (data: CreateMessageRequest) => {
  return http.post<{ id: number }>("/api/messages/create", data);
};

export const markAsRead = (id: number) => {
  return http.post(`/api/messages/${id}/read`);
};

export const markAllAsRead = () => {
  return http.post("/api/messages/read-all");
};

export const deleteMessage = (id: number) => {
  return http.post(`/api/messages/${id}/delete`);
};
