export interface HistoryClusterInfo {
  name: string;
  namespace: string;
  sessionName: string;
  createTime?: string;
  createTimeStamp?: number;
}

export type HistoryClusterInfoList = HistoryClusterInfo[];

export interface HistoryTask {
  task_id: string;
  name: string;
  attempt_number: number;
  state: string;
  job_id: string;
  node_id: string;
  actor_id: string;
  placement_group_id: string;
  type: string;
  func_or_class_name: string;
  language: string;
  required_resources: Record<string, number> | null;
  worker_id: string;
  error_type: string;
  error_message: string;
  call_site: string;
  start_time?: number;
  end_time?: number;
}

export interface HistoryTasksResponse {
  result?: boolean;
  msg?: string;
  data: {
    result: {
      result: HistoryTask[];
      total: number;
      num_filtered: number;
      num_after_truncation: number;
      partial_failure_warning?: string;
      warnings?: unknown;
    };
  };
}
