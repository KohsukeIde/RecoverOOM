package RecoverOOM

import (
	"flag"
	"fmt"
	"os"
	"regexp"
)

//GOのデータ構造を使用してYAMLの内容を操作(relies on YAML package's formatting rules)
// func main() {
// 	var filePath string
// 	var increaseRate float64

// 	flag.StringVar(&filePath, "file", "local-cronJob.yaml", "Path to the YAML file")
// 	flag.Float64Var(&increaseRate, "rate", 1.2, "Rate to increase memory request and limit")
// 	flag.Parse()

// 	//load Yaml file
// 	fileContent, err := os.ReadFile(filePath)
// 	if err != nil {
// 		fmt.Printf("Error reading YAML file: %s\n", err)
// 		return
// 	}

// 	fmt.Println(string(fileContent))

// 	var data map[string]interface{}
// 	err = yaml.Unmarshal(fileContent, &data)
// 	if err != nil {
// 		fmt.Printf("Error decoding YAML: %s\n", err)
// 		return
// 	}

// 	containers := extractContainer(data)
// 	for _, container := range containers {
// 		resources, ok := container["resources"].(map[string]interface{})
// 		if !ok {
// 			continue
// 		}
// 		updateMemory(resources, "requests", increaseRate)
// 		updateMemory(resources, "limits", increaseRate)
// 	}

// 	//FIX: generates indents that doesnt exist in the original format
// 	newYAML, err := yaml.Marshal(data)
// 	if err != nil {
// 		fmt.Printf("Error writing YAML: %s\n", err)
// 		return
// 	}

// 	err = os.WriteFile(filePath, newYAML, os.ModePerm)
// 	if err != nil {
// 		fmt.Printf("Error writing YAML file: %s\n", err)
// 		return
// 	}
// 	fmt.Println("Updated the cronjob YAML successfully!")
// }

// func extractContainer(data map[string]interface{}) []map[string]interface{} {
// 	spec, ok := data["spec"].(map[string]interface{})
// 	if !ok {
// 		return nil
// 	}

// 	jobTemplate, ok := spec["jobTemplate"].(map[string]interface{})
// 	if !ok {
// 		return nil
// 	}

// 	specInner1, ok := jobTemplate["spec"].(map[string]interface{})
// 	if !ok {
// 		return nil
// 	}

// 	template, ok := specInner1["template"].(map[string]interface{})
// 	if !ok {
// 		return nil
// 	}

// 	specInner2, ok := template["spec"].(map[string]interface{})
// 	if !ok {
// 		return nil
// 	}

// 	containers, ok := specInner2["containers"].([]interface{})
// 	if !ok {
// 		return nil
// 	}

// 	var results []map[string]interface{}
// 	for _, container := range containers {
// 		containerMap, ok := container.(map[string]interface{})
// 		if ok {
// 			results = append(results, containerMap)
// 		}
// 	}
// 	return results
// }

// func updateMemory(resources map[string]interface{}, key string, rate float64) {
// 	memoryVal, ok := resources[key].(map[string]interface{})["memory"].(string)
// 	if !ok {
// 		return
// 	}

// 	var memory int
// 	fmt.Sscanf(memoryVal, "%d", &memory)
// 	newMemory := float64(memory) * rate

// 	resources[key].(map[string]interface{})["memory"] = fmt.Sprintf("%.0fMi", newMemory)
// }

// Solved problem with indents. MIGHT NOT WORK ON CERTAIN FORMAT(複数の)
func main() {
	var filePath string
	var increaseRate float64

	flag.StringVar(&filePath, "file", "local-cronJob.yaml", "Path to the YAML file")
	flag.Float64Var(&increaseRate, "rate", 1.2, "Rate to increase memory request and limit")
	flag.Parse()

	//load Yaml file
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading YAML file: %s\n", err)
		return
	}

	fmt.Println(string(fileContent))

	updatedContent, oldMemory, newMemory, unit := updateMemoryInYamlRate(string(fileContent), increaseRate)
	err = os.WriteFile(filePath, []byte(updatedContent), os.ModePerm)
	if err != nil {
		fmt.Printf("Error writing YAML: %s\n", err)
		return
	}
	fmt.Printf("Memory was updated from %d%s to %d%s\n", oldMemory, unit, newMemory, unit)
}

func updateMemoryInYamlRate(yamlContent string, rate float64) (string, int, int, string) {
	var oldMemory, newMemoryInt int
	var memoryUnit string

	re := regexp.MustCompile(`(memory:\s*)(\d+)(Mi|Gi|Ki)`)
	updatedContent := re.ReplaceAllStringFunc(yamlContent, func(match string) string {
		var memory int
		var unit string

		fmt.Sscanf(match, "memory: %d%s", &memory, &unit)
		memoryUnit = unit
		newMemory := float64(memory) * rate
		newMemoryInt = int(newMemory)
		oldMemory = memory
		return fmt.Sprintf("memory: %d%s", newMemoryInt, unit)
	})
	return updatedContent, oldMemory, newMemoryInt, memoryUnit
}

// Below is function used in batch.go
func RunUpdateMemoryInYaml(inputFilePath string) {
	fileContent, err := os.ReadFile(inputFilePath)
	if err != nil {
		fmt.Printf("Error reading YAML file: %s\n", err)
		return
	}
	fmt.Println(string(fileContent))

	updatedContent, oldMemory, newMemory, unit := updateMemoryInYamlValue(string(fileContent), 2.0)
	err = os.WriteFile(inputFilePath, []byte(updatedContent), os.ModePerm)
	if err != nil {
		fmt.Printf("Error writing YAML: %s\n", err)
		return
	}
	fmt.Printf("Memory was updated from %d%s to %d%s\n", oldMemory, unit, newMemory, unit)
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
		newMemory := float64(memory) + value
		newMemoryInt = int(newMemory)
		oldMemory = memory
		return fmt.Sprintf("memory: %d%s", newMemoryInt, unit)
	})
	return updatedContent, oldMemory, newMemoryInt, memoryUnit
}
