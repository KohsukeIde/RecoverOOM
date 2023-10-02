package RecoverOOM

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/google/uuid"

	container "google.golang.org/api/container/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd/api"
)

type Option struct {
	ProjectID string

	MaxDisplaySameErrors int

	Username  string
	IconEmoji string

	WebhookUrl   string
	ChannelFatal string
	ChannelDebug string
}

type involvedObject struct {
	Namespace bigquery.NullString `bigquery:"namespace"`
	Name      bigquery.NullString `bigquery:"name"`
	Kind      bigquery.NullString `bigquery:"kind"`
}

type jsonPayload struct {
	Reason         bigquery.NullString `bigquery:"reason"`
	InvolvedObject involvedObject      `bigquery:"involvedObject"`
	Message        bigquery.NullString `bigquery:"message"`
}

type resourceLabels struct {
	ProjectID   bigquery.NullString `bigquery:"project_id"`
	Location    bigquery.NullString `bigquery:"location"`
	ClusterName bigquery.NullString `bigquery:"cluster_name"`
}

type resource struct {
	Labels resourceLabels `bigquery:"labels"`
}

type event struct {
	JsonPayload *jsonPayload           `bigquery:"jsonPayload"`
	TextPayload bigquery.NullString    `bigquery:"textPayload"`
	Resource    resource               `bigquery:"resource"`
	Timestamp   bigquery.NullTimestamp `bigquery:"timestamp"`
}

type NamespaceName struct {
	Namespace string
	Name      string
}

type OOMKilledPodInfo struct {
	Pod v1.Pod
}

type CronJobInfo struct {
	CronJob      batchv1.CronJob
	Pods         []OOMKilledPodInfo
	maxPodMemory string
	cluster      *container.Cluster
}

var ErrNotAssociatedWithCronJob = errors.New("pod is not associated with a CronJob")

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func getCronJobFromPod(ctx context.Context, cluster *container.Cluster, pod v1.Pod) (*batchv1.CronJob, error) {
	clientset, err := newClientset(ctx, cluster)
	if err != nil {
		return nil, err
	}

	controllerRef := metav1.GetControllerOf(&pod)
	if controllerRef == nil || controllerRef.Kind != "Job" {
		return nil, ErrNotAssociatedWithCronJob
	}

	job, err := clientset.BatchV1().Jobs(pod.Namespace).Get(ctx, controllerRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, ErrNotAssociatedWithCronJob
	}

	cronJobRef := metav1.GetControllerOf(job)
	if cronJobRef == nil || cronJobRef.Kind != "CronJob" {
		return nil, ErrNotAssociatedWithCronJob
	}

	cronJob, err := clientset.BatchV1().CronJobs(pod.Namespace).Get(ctx, cronJobRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return cronJob, nil
}

func getCronJobYAML(cronJobName, namespace, dir string) (string, error) {
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		panic(err)
	}

	cmd := exec.Command("kubectl", "get", "cronjob", cronJobName, "-n", namespace, "-o", "yaml")

	fileName := fmt.Sprintf("%s.yaml", cronJobName)
	filePath := filepath.Join(dir, fileName)
	outfile, err := os.Create(filePath)
	if err != nil {
		return "", err
	}

	defer outfile.Close()
	cmd.Stdout = outfile
	err = cmd.Run()
	return filePath, err
}

func runUpdateMemoryInYaml(inputFilePath string) (int, string) {
	fileContent, err := os.ReadFile(inputFilePath)
	if err != nil {
		fmt.Printf("Error reading YAML file: %s\n", err)
		return 0, ""
	}
	fmt.Println(string(fileContent))

	updatedContent, oldMemory, newMemory, unit := updateMemoryInYamlValue(string(fileContent), 2.0)
	err = os.WriteFile(inputFilePath, []byte(updatedContent), os.ModePerm)
	if err != nil {
		fmt.Printf("Error writing YAML: %s\n", err)
		return 0, ""
	}
	fmt.Printf("Memory is updating from %d%s to %d%s\n", oldMemory, unit, newMemory, unit)
	return oldMemory, unit
}

func updateMemoryInYamlValue(yamlContent string, value float64) (string, int, int, string) {
	var oldMemory, newMemoryInt int
	var memoryUnit string

	re := regexp.MustCompile(`(memory:\s*)(\d+)(Mi|Gi|Ki)`)
	updatedContent := re.ReplaceAllStringFunc(yamlContent, func(match string) string {
		var memory int
		var unit string

		fmt.Sscanf(match, "memory: %d%s", &memory, &unit)
		memoryUnit = unit
		newMemory := float64(memory) * value
		newMemoryInt = int(newMemory)
		oldMemory = memory
		return fmt.Sprintf("memory: %d%s", newMemoryInt, unit)
	})
	return updatedContent, oldMemory, newMemoryInt, memoryUnit
}

func applyChangeYaml(changedYAMLPath string) {
	cmd := exec.Command("kubectl", "apply", "-f", changedYAMLPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println("applychangeyaml error", stderr.String())
		panic(err)
	}
	fmt.Println("Done with applying changes")
}

func retrieveOOMKilledPodsInAllClusters(ctx context.Context, projectId, functionRegion, clusterName string) ([]v1.Pod, error) {
	containerService, err := container.NewService(ctx)
	if err != nil {
		return nil, err
	}

	parent := fmt.Sprintf("projects/%s/locations/%s", projectId, functionRegion) //FUNCTION_REGION=asia-northeast1
	res, err := containerService.Projects.Locations.Clusters.List(parent).Do()
	if err != nil {
		return nil, err
	}

	clusterNamesMap := make(map[string]struct{})
	clusterNamesMap[clusterName] = struct{}{}

	fmt.Printf("CLUSTER NAME MAP: %#v\n", clusterNamesMap)

	oomKilledPods := make([]v1.Pod, 0)
	cronJobsMap := make(map[string]CronJobInfo)

	for _, cluster := range res.Clusters {
		_, exists := clusterNamesMap[cluster.Name]
		fmt.Printf("EXISTS: %#v\n", exists)
		if exists {
			pods, err := retrieveOOMKilledPods(ctx, cluster)
			if err != nil {
				return nil, err
			}
			for _, pod := range pods {
				fmt.Printf("POD CONTAINER LENGTH %#v\n", len(pod.Spec.Containers))

				cronJob, err := getCronJobFromPod(ctx, cluster, pod)
				if err == ErrNotAssociatedWithCronJob {
					continue
				} else if err != nil {
					return nil, err
				}

				info, exists := cronJobsMap[cronJob.Name]
				if !exists {
					info = CronJobInfo{
						CronJob: *cronJob,
					}
				}

				info.Pods = append(info.Pods, OOMKilledPodInfo{Pod: pod})
				info.cluster = cluster
				getPodMaxMemory(pod, &info)
				cronJobsMap[cronJob.Name] = info
			}
			oomKilledPods = append(oomKilledPods, pods...)
		}
	}

	for key := range cronJobsMap {
		// fmt.Printf("cronJobMap key: %#v\n", key)
		// fmt.Printf("CronJob NAME: %#v\n", cronJobsMap[key].CronJob.Name)
		// fmt.Printf("CronJob maxPodMem: %#v\n", cronJobsMap[key].maxPodMemory)
		filePath, err := getCronJobYAML(key, cronJobsMap[key].CronJob.Namespace, "cronjobs")
		if err != nil {
			return nil, err
		}

		oldMemory, memoryUnit := runUpdateMemoryInYaml(filePath)

		// fmt.Println("CRONJOBMAP CHEEEECK:", cronJobsMap[key])
		// fmt.Println("FMT SPRINTF CHEEEECK", fmt.Sprintf("%d%s", oldMemory, memoryUnit))
		fmt.Printf("maxPodMemory: %#v\n", cronJobsMap[key].maxPodMemory)
		fmt.Printf("oldMemory: %#v\n", fmt.Sprintf("%d%s", oldMemory, memoryUnit))
		if cronJobsMap[key].maxPodMemory == fmt.Sprintf("%d%s", oldMemory, memoryUnit) {

			fmt.Println("APPLYING CHANGES")
			applyChangeYaml(filePath)
			// this is where im supposed instantly execute new job based on new manifest

			jobName, err := createJob(cronJobsMap[key].CronJob, cronJobsMap[key].CronJob.Namespace)
			if err != nil {
				return nil, err
			}
			fmt.Printf("Manually created Job: %s at %#v\n", jobName, time.Now())
		} else {
			fmt.Println("WAITING FOR PREVIOUS CHANGES TO BE APPLIED")
		}
	}

	return oomKilledPods, err
}

func retrieveOOMKilledPods(ctx context.Context, cluster *container.Cluster) ([]v1.Pod, error) {
	fmt.Println("retrieveOOMKilledPods CALLED")
	// fmt.Printf("CLUSTERNAME WITHIN FUNC: %#v\n", cluster.Name)
	clientset, err := newClientset(ctx, cluster)
	if err != nil {
		return nil, err
	}

	podList, err := clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
		FieldSelector: "status.phase=Failed",
	})
	if err != nil {
		return nil, err
	}

	oomKilledPods := make([]v1.Pod, 0)
	for _, pod := range podList.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Terminated != nil && status.State.Terminated.Reason == "OOMKilled" {
				oomKilledPods = append(oomKilledPods, pod)
				break
			}
		}
	}
	// fmt.Println("len of OOMKilledpods: ", len(oomKilledPods))

	sort.Slice(oomKilledPods, func(i, j int) bool {
		return oomKilledPods[i].CreationTimestamp.After(oomKilledPods[j].CreationTimestamp.Time)
	})
	return oomKilledPods, nil
}

func getPodMaxMemory(pod v1.Pod, cronJobInfo *CronJobInfo) error {
	memoryCandidates := make([]string, 0)

	for _, container := range pod.Spec.Containers {
		limits := container.Resources.Limits
		fmt.Printf("containers: %+v\n", container)
		// fmt.Printf("Container %d Memory Limits : %#v\n", i, getPodMemory(limits[v1.ResourceMemory]))
		memoryCandidates = append(memoryCandidates, getPodMemory(limits[v1.ResourceMemory]))
	}

	maxMem, err := maxMemory(memoryCandidates)
	if err != nil {
		return err
	}
	// fmt.Println("CRONJOB MAX MEMORRRYYY", maxMem)
	cronJobInfo.maxPodMemory = maxMem
	// fmt.Println("CRONJOB MAX MEMORRRYYY", cronJobInfo.maxPodMemory)
	return nil
}

func getPodMemory(x interface{}) string {
	fmt.Printf("type X%#v\n", x)
	rv := reflect.ValueOf(x)
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Type().Field(i)
		fv := rv.FieldByName(f.Name)
		if f.Name == "s" {
			fmt.Printf("print fv.string%#v\n", fv.String())
			fmt.Printf("print fv%#v\n", fv)
			return fv.String()
		}
	}
	panic(fmt.Sprintf("end of getpodmemory. type of x is %#v\n", x))
}

func maxMemory(memList []string) (string, error) {
	var maxMem string
	var maxBytes int64

	for _, mem := range memList {
		bytes, err := toBytes(mem)
		if err != nil {
			return "", err
		}
		if bytes > maxBytes {
			maxMem = mem
			maxBytes = bytes
		}
	}
	return maxMem, nil
}

func toBytes(mem string) (int64, error) {
	mem = strings.ToLower(mem)
	fmt.Printf("mem print %+v\n", mem)
	unit := mem[len(mem)-2:]
	value, err := strconv.Atoi(mem[:len(mem)-2])
	if err != nil {
		return 0, err
	}

	switch unit {
	case "ki":
		return int64(value) * 1024, nil
	case "mi":
		return int64(value) * 1024 * 1024, nil
	case "gi":
		return int64(value) * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("unknown unit : %s", unit)
	}
}

func createJob(cronJob batchv1.CronJob, namespace string) (string, error) {
	// fmt.Println("INSIDE CREATEJOB")
	jobName := fmt.Sprintf("ide-manual-%s", uuid.New())
	cmd := exec.Command("kubectl", "create", "job", jobName, fmt.Sprintf("--from=cronjob/%s", cronJob.Name), "-n", namespace)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	fmt.Println(stderr.String())
	if err != nil {
		fmt.Println("errRERrererer", err)
		return "", err
	}
	return jobName, nil
}

// reference: https://github.com/kubernetes/client-go/issues/424#issuecomment-718231274
// He says that the `WrapTransport` must be supplied, but it actually works without it.
func newClientset(ctx context.Context, cluster *container.Cluster) (*kubernetes.Clientset, error) {
	caData, err := base64.StdEncoding.DecodeString(cluster.MasterAuth.ClusterCaCertificate)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(&rest.Config{
		Host: cluster.Endpoint,
		AuthProvider: &api.AuthProviderConfig{
			Name: "gcp",
		},
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: false,
			CAData:   caData,
		},
	})
}

// Runs the entire process of fetching Cronjob yaml
// queryEvents -> []event
// retrieveOOMKilledPodsInAllClusters -> []v1.Pods
// getCronJobsFromOOMKilledPods
func Run(ctx context.Context, option Option, projectId, functionRegion, clusterName string) error {
	fmt.Println("OOM recovery CALLED at:", time.Now())
	_, err := retrieveOOMKilledPodsInAllClusters(ctx, projectId, functionRegion, clusterName)
	// fmt.Printf("len of killed pods in all clusters: %#v\n", len(oomKilledPods))
	// fmt.Printf("first killed pods in all clusters%#v\n", oomKilledPods[0])
	if err != nil {
		return fmt.Errorf("error occured in retrieveOOMKilledPods: %w", err)
	}
	return nil
}
