Prometheus exporter for exporting metric from instances registered to ecs cluster and ecs services running on EC2.
Exported metrics :

- task_def_version - Current task def version of the service. 
- desired_count - Current desired_count for the service.
- running_count - Current running count for the service.
- failed_count - Number of deployment falures in current PRIMARY deployment
- cpu_remaining - The number of cpu units remaning on a cluster instance
- memory_remaining - The memory remaining on a cluster instance
- running_tasks - The number of tasks currently running on the instance
- pending_tasks - The number of tasks currently pending on the instance
- cpu_per_task -  The cpu shares required for the current PRIMARY deploymnt of the task.
- memory_per_task - The memory reserved for the current PRIMARY deployment of the task.
