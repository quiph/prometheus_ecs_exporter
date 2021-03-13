package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	mux           *sync.Mutex
	handleFunc    http.Handler
	taskDefMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "deployments",
		Name:      "task_def_version",
		Help:      "Task def version of the service.",
	},
		[]string{
			"service",
			"taskdef",
			"state",
			"cluster",
		})

	desiredCountMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "deployments",
		Name:      "desired_count",
		Help:      "Desired count for the service.",
	},
		[]string{
			"service",
			"taskdef",
			"state",
			"cluster",
		})

	runningCountMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "deployments",
		Name:      "running_count",
		Help:      "Running tasks count for the service.",
	},
		[]string{
			"service",
			"taskdef",
			"state",
			"cluster",
		})

	failedCountMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "deployments",
		Name:      "failed_count",
		Help:      "Currently failing count for service deployment.",
	},
		[]string{
			"service",
			"taskdef",
			"state",
			"cluster",
		})

	cpuLeftPerInstanceMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "instance",
		Name:      "cpu_remaining",
		Help:      "Amount of available cpu for scheduling tasks.",
	},
		[]string{
			"containerInstance",
			"cluster",
			"agent_connected",
			"status",
		})

	memoryLeftPerInstanceMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "instance",
		Name:      "memory_remaining",
		Help:      "Amount of available memory for scheduling tasks.",
	},
		[]string{
			"containerInstance",
			"cluster",
			"agent_connected",
			"status",
		})

	runningTasksPerInstanceMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "instance",
		Name:      "running_tasks",
		Help:      "Number of tasks currently running on the instance.",
	},
		[]string{
			"containerInstance",
			"cluster",
			"agent_connected",
			"status",
		})

	pendingTasksPerInstanceMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "instance",
		Name:      "pending_tasks",
		Help:      "Number of tasks currently pending on the instance .",
	},
		[]string{
			"containerInstance",
			"cluster",
			"agent_connected",
			"status",
		})

	taskCpuMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "deployments",
		Name:      "cpu_per_task",
		Help:      "Cpu reserved for the currently primary task def.",
	},
		[]string{
			"service",
			"cluster",
		})

	taskMemoryMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ecs",
		Subsystem: "deployments",
		Name:      "memory_per_task",
		Help:      "Memory reserved for the currently primary task def.",
	},
		[]string{
			"service",
			"cluster",
		})
)

func handleErr(er error) {
	if aerr, ok := er.(awserr.Error); ok {
		switch aerr.Code() {
		case ecs.ErrCodeServerException:
			fmt.Println(ecs.ErrCodeServerException, aerr.Error())
		case ecs.ErrCodeClientException:
			fmt.Println(ecs.ErrCodeClientException, aerr.Error())
		case ecs.ErrCodeInvalidParameterException:
			fmt.Println(ecs.ErrCodeInvalidParameterException, aerr.Error())
		default:
			fmt.Println(aerr.Error())
		}
	} else {
		fmt.Println(er)
		os.Exit(1)
	}
}

func setContainerInstanceMetrics(cluster *string, svc *ecs.ECS) {
	inputContainerInstanceList := &ecs.ListContainerInstancesInput{
		Cluster: cluster,
	}
	resultContainerInstanceList, err := svc.ListContainerInstances(inputContainerInstanceList)
	if err != nil {
		handleErr(err)
	}
	inputDescribeContainerInstance := &ecs.DescribeContainerInstancesInput{
		Cluster:            cluster,
		ContainerInstances: resultContainerInstanceList.ContainerInstanceArns,
	}
	resultContainerInstanceDescription, err := svc.DescribeContainerInstances(inputDescribeContainerInstance)
	if err != nil {
		handleErr(err)
	}

	for _, containerInstance := range resultContainerInstanceDescription.ContainerInstances {

		containerInstanceRelativeTemp := strings.Split(*containerInstance.ContainerInstanceArn, "/")
		containerInstanceRelative := containerInstanceRelativeTemp[len(containerInstanceRelativeTemp)-1]
		pendingTasksPerInstanceMetric.With(prometheus.Labels{"containerInstance": containerInstanceRelative, "cluster": *cluster, "agent_connected": strconv.FormatBool(*containerInstance.AgentConnected), "status": *containerInstance.Status}).Set(float64(*containerInstance.PendingTasksCount))
		runningTasksPerInstanceMetric.With(prometheus.Labels{"containerInstance": containerInstanceRelative, "cluster": *cluster, "agent_connected": strconv.FormatBool(*containerInstance.AgentConnected), "status": *containerInstance.Status}).Set(float64(*containerInstance.RunningTasksCount))
		for _, remainingResources := range containerInstance.RemainingResources {
			if *remainingResources.Name == "CPU" {
				cpuLeftPerInstanceMetric.With(prometheus.Labels{"containerInstance": containerInstanceRelative, "cluster": *cluster, "agent_connected": strconv.FormatBool(*containerInstance.AgentConnected), "status": *containerInstance.Status}).Set(float64(*remainingResources.IntegerValue))

			} else if *remainingResources.Name == "MEMORY" {
				memoryLeftPerInstanceMetric.With(prometheus.Labels{"containerInstance": containerInstanceRelative, "cluster": *cluster, "agent_connected": strconv.FormatBool(*containerInstance.AgentConnected), "status": *containerInstance.Status}).Set(float64(*remainingResources.IntegerValue))

			}

		}
	}

}

func setServiceSpecificMetrics(cluster *string, svc *ecs.ECS) {
	var maxRes int64 = 100
	inputServiceList := &ecs.ListServicesInput{Cluster: cluster, MaxResults: &maxRes}
	resultServiceList, err := svc.ListServices(inputServiceList)
	if err != nil {
		handleErr(err)
	}
	stepSize := 9
	totalLength := len(resultServiceList.ServiceArns)
	loops := totalLength / stepSize
	var end int
	for i := 0; i <= loops; i++ {
		start := i * stepSize
		if i < loops {
			end = (i + 1) * stepSize
		} else {
			end = totalLength
		}
		inputServiceDescribe := &ecs.DescribeServicesInput{
			Services: resultServiceList.ServiceArns[start:end],
			Cluster:  cluster,
		}
		resultServiceDescribe, err := svc.DescribeServices(inputServiceDescribe)
		if err != nil {
			handleErr(err)

		}
		for _, tempService := range resultServiceDescribe.Services {
			for _, deployments := range tempService.Deployments {
				taskDefFull := *deployments.TaskDefinition
				taskDefRelativeTemp := strings.Split(taskDefFull, "/")
				taskDefRelative := taskDefRelativeTemp[len(taskDefRelativeTemp)-1]
				taskDefSplit := strings.Split(taskDefRelative, ":")
				taskDefName := taskDefSplit[0]
				taskDefVersion := taskDefSplit[1]
				taskDefVersionConverted, _ := strconv.ParseFloat(taskDefVersion, 64)
				if *deployments.Status == "PRIMARY" {
					inputTaskDefDescribe := &ecs.DescribeTaskDefinitionInput{
						TaskDefinition: deployments.TaskDefinition,
					}
					resultTaskDefDescribe, err := svc.DescribeTaskDefinition(inputTaskDefDescribe)
					if err != nil {
						handleErr(err)
					}
					var totalCpuForTask int64
					var totalMemoryForTask int64
					for _, container := range resultTaskDefDescribe.TaskDefinition.ContainerDefinitions {
						if container.Cpu != nil {
							totalCpuForTask += *container.Cpu
						}
						if container.MemoryReservation != nil {
							totalMemoryForTask += *container.MemoryReservation
						}
					}
					taskCpuMetric.With(prometheus.Labels{"service": *tempService.ServiceName, "cluster": *cluster}).Set(float64(totalCpuForTask))
					taskMemoryMetric.With(prometheus.Labels{"service": *tempService.ServiceName, "cluster": *cluster}).Set(float64(totalMemoryForTask))

				}

				taskDefMetric.With(prometheus.Labels{"service": *tempService.ServiceName, "taskdef": taskDefName, "state": *deployments.Status, "cluster": *cluster}).Set(taskDefVersionConverted)
				desiredCountMetric.With(prometheus.Labels{"service": *tempService.ServiceName, "taskdef": taskDefName, "state": *deployments.Status, "cluster": *cluster}).Set(float64(*deployments.DesiredCount))
				runningCountMetric.With(prometheus.Labels{"service": *tempService.ServiceName, "taskdef": taskDefName, "state": *deployments.Status, "cluster": *cluster}).Set(float64(*deployments.RunningCount))
				failedCountMetric.With(prometheus.Labels{"service": *tempService.ServiceName, "taskdef": taskDefName, "state": *deployments.Status, "cluster": *cluster}).Set(float64(*deployments.FailedTasks))

			}

		}
	}

}
func pollMetrics(region string, scrapeInterval int) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region)},
	)
	if err != nil {
		handleErr(err)
	}
	svc := ecs.New(sess)
	for true {
		inputClusterList := &ecs.ListClustersInput{}

		resultClusterList, err := svc.ListClusters(inputClusterList)
		if err != nil {
			handleErr(err)
		}
		mux.Lock()
		pendingTasksPerInstanceMetric.Reset()
		runningTasksPerInstanceMetric.Reset()
		cpuLeftPerInstanceMetric.Reset()
		memoryLeftPerInstanceMetric.Reset()
		for _, clusterName := range resultClusterList.ClusterArns {
			clusterNameFull := *clusterName
			clusterNameRelativeSplit := strings.Split(clusterNameFull, "/")
			clusterNameRelatvie := clusterNameRelativeSplit[len(clusterNameRelativeSplit)-1]
			setContainerInstanceMetrics(&clusterNameRelatvie, svc)
			setServiceSpecificMetrics(&clusterNameRelatvie, svc)

		}
		mux.Unlock()
		time.Sleep(time.Duration(scrapeInterval) * time.Second)

	}
}

func handleWithLocking(w http.ResponseWriter, r *http.Request) {
	mux.Lock()
	defer mux.Unlock()

	handleFunc.ServeHTTP(w, r)
}

func main() {
	mux = &sync.Mutex{}
	scrapeInterval := 120
	region, presentRegion := os.LookupEnv("region")
	if !presentRegion {
		fmt.Println("Region not specified defaulting to ap-south-1")
		region = "ap-south-1"
	}
	interval, presentScrapeInterval := os.LookupEnv("scrape_interval")
	if !presentScrapeInterval {
		fmt.Println("No scrape interval provided, defaulting to 120 seconds")
	} else {
		var err error
		scrapeInterval, err = strconv.Atoi(interval)
		if err != nil {
			handleErr(err)
		}
	}
	go pollMetrics(region, scrapeInterval)
	prometheus.MustRegister(taskDefMetric, desiredCountMetric, runningCountMetric, failedCountMetric, memoryLeftPerInstanceMetric, cpuLeftPerInstanceMetric, runningTasksPerInstanceMetric, pendingTasksPerInstanceMetric, taskCpuMetric, taskMemoryMetric)
	handleFunc = promhttp.Handler()
	http.HandleFunc("/metrics", handleWithLocking)
	http.ListenAndServe(":2112", nil)
}
