package azuredevops

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/microsoft/terraform-provider-azuredevops/azuredevops/utils/config"
	"github.com/microsoft/terraform-provider-azuredevops/azuredevops/utils/converter"
	"github.com/microsoft/terraform-provider-azuredevops/azuredevops/utils/tfhelper"
	"github.com/microsoft/terraform-provider-azuredevops/azuredevops/utils/validate"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"
	"github.com/microsoft/azure-devops-go-api/azuredevops/build"
)

func resourceBuildDefinition() *schema.Resource {
	return &schema.Resource{
		Create: resourceBuildDefinitionCreate,
		Read:   resourceBuildDefinitionRead,
		Update: resourceBuildDefinitionUpdate,
		Delete: resourceBuildDefinitionDelete,
		Importer: &schema.ResourceImporter{
			State: func(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
				projectID, buildDefinitionID, err := ParseImportedProjectIDAndID(meta.(*config.AggregatedClient), d.Id())
				if err != nil {
					return nil, fmt.Errorf("Error parsing the build definition ID from the Terraform resource data: %v", err)
				}
				d.Set("project_id", projectID)
				d.SetId(fmt.Sprintf("%d", buildDefinitionID))

				return []*schema.ResourceData{d}, nil
			},
		},
		Schema: map[string]*schema.Schema{
			"project_id": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The ID of the project in which to create the definition",
			},
			"revision": {
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "Revision number of the definition",
			},
			"name": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "Name of the definition",
			},
			"path": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      `\`,
				ValidateFunc: validate.Path,
				Description:  "Path of the definition",
			},
			"variable_groups": {
				Type: schema.TypeSet,
				Elem: &schema.Schema{
					Type:         schema.TypeInt,
					ValidateFunc: validation.IntAtLeast(1),
				},
				MinItems:    1,
				Optional:    true,
				Description: "Variable groups linked to the definition",
			},
			"agent_pool_name": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "Hosted Ubuntu 1604",
				Description: "Agent pool in which to run the definition",
			},
			"repository": {
				Type:        schema.TypeSet,
				Required:    true,
				MinItems:    1,
				MaxItems:    1,
				Description: "Repository in which the YAML based definition exists",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"yml_path": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Relative path of YAML file describing the definition",
						},
						"repo_name": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "Name of the repository hosting the YAML",
						},
						"repo_type": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice([]string{"GitHub", "TfsGit"}, false),
							Description:  "Type of repository hosting the YAML",
						},
						"branch_name": {
							Type:        schema.TypeString,
							Optional:    true,
							Default:     "master",
							Description: "Name of branch to look for YAML in",
						},
						"service_connection_id": {
							Type:        schema.TypeString,
							Optional:    true,
							Default:     "",
							Description: "Service connection that grants access to the repo hosting the YAML",
						},
					},
				},
			},
		},
	}
}

func resourceBuildDefinitionCreate(d *schema.ResourceData, meta interface{}) error {
	clients := meta.(*config.AggregatedClient)
	buildDefinition, projectID, err := expandBuildDefinition(d)
	if err != nil {
		return fmt.Errorf("Error creating resource Build Definition: %+v", err)
	}

	createdBuildDefinition, err := createBuildDefinition(clients, buildDefinition, projectID)
	if err != nil {
		return fmt.Errorf("Error creating resource Build Definition: %+v", err)
	}

	err = flattenBuildDefinition(d, createdBuildDefinition, projectID)
	if err != nil {
		return fmt.Errorf("Error flattening Build Definition: %+v", err)
	}
	return resourceBuildDefinitionRead(d, meta)
}

func flattenBuildDefinition(d *schema.ResourceData, buildDefinition *build.BuildDefinition, projectID string) error {
	d.SetId(strconv.Itoa(*buildDefinition.Id))
	d.Set("project_id", projectID)
	d.Set("name", buildDefinition.Name)
	d.Set("path", buildDefinition.Path)
	d.Set("agent_pool_name", buildDefinition.Queue.Pool.Name)

	revision := 0
	if buildDefinition.Revision != nil {
		revision = *buildDefinition.Revision
	}

	d.Set("revision", revision)

	err := d.Set("repository", flattenRepository(buildDefinition))
	if err != nil {
		return err
	}
	err = d.Set("variable_groups", flattenVariableGroups(buildDefinition))
	if err != nil {
		return err
	}
	return nil
}

func flattenVariableGroups(buildDefinition *build.BuildDefinition) []int {
	if buildDefinition.VariableGroups == nil {
		return nil
	}

	variableGroups := make([]int, len(*buildDefinition.VariableGroups))

	for i, variableGroup := range *buildDefinition.VariableGroups {
		variableGroups[i] = *variableGroup.Id
	}

	return variableGroups
}

func createBuildDefinition(clients *config.AggregatedClient, buildDefinition *build.BuildDefinition, project string) (*build.BuildDefinition, error) {
	createdBuild, err := clients.BuildClient.CreateDefinition(clients.Ctx, build.CreateDefinitionArgs{
		Definition: buildDefinition,
		Project:    &project,
	})

	return createdBuild, err
}

func resourceBuildDefinitionRead(d *schema.ResourceData, meta interface{}) error {
	clients := meta.(*config.AggregatedClient)
	projectID, buildDefinitionID, err := tfhelper.ParseProjectIDAndResourceID(d)

	if err != nil {
		return err
	}

	buildDefinition, err := clients.BuildClient.GetDefinition(clients.Ctx, build.GetDefinitionArgs{
		Project:      &projectID,
		DefinitionId: &buildDefinitionID,
	})

	if err != nil {
		return err
	}

	err = flattenBuildDefinition(d, buildDefinition, projectID)
	if err != nil {
		return fmt.Errorf("Error flattening Build Definition: %+v", err)
	}
	return nil
}

func resourceBuildDefinitionDelete(d *schema.ResourceData, meta interface{}) error {
	if d.Id() == "" {
		return nil
	}

	clients := meta.(*config.AggregatedClient)
	projectID, buildDefinitionID, err := tfhelper.ParseProjectIDAndResourceID(d)
	if err != nil {
		return err
	}

	err = clients.BuildClient.DeleteDefinition(meta.(*config.AggregatedClient).Ctx, build.DeleteDefinitionArgs{
		Project:      &projectID,
		DefinitionId: &buildDefinitionID,
	})

	return err
}

func resourceBuildDefinitionUpdate(d *schema.ResourceData, meta interface{}) error {
	clients := meta.(*config.AggregatedClient)
	buildDefinition, projectID, err := expandBuildDefinition(d)
	if err != nil {
		return err
	}

	updatedBuildDefinition, err := clients.BuildClient.UpdateDefinition(meta.(*config.AggregatedClient).Ctx, build.UpdateDefinitionArgs{
		Definition:   buildDefinition,
		Project:      &projectID,
		DefinitionId: buildDefinition.Id,
	})

	if err != nil {
		return err
	}

	err = flattenBuildDefinition(d, updatedBuildDefinition, projectID)
	if err != nil {
		return fmt.Errorf("Error flattening Build Definition: %+v", err)
	}
	return nil
}

func flattenRepository(buildDefiniton *build.BuildDefinition) interface{} {
	yamlFilePath := ""

	// The process member can be of many types -- the only typing information
	// available from the compiler is `interface{}` so we can probe for known
	// implementations
	if processMap, ok := buildDefiniton.Process.(map[string]interface{}); ok {
		yamlFilePath = processMap["yamlFilename"].(string)
	}

	if yamlProcess, ok := buildDefiniton.Process.(*build.YamlProcess); ok {
		yamlFilePath = *yamlProcess.YamlFilename
	}

	return []map[string]interface{}{{
		"yml_path":              yamlFilePath,
		"repo_name":             *buildDefiniton.Repository.Name,
		"repo_type":             *buildDefiniton.Repository.Type,
		"branch_name":           *buildDefiniton.Repository.DefaultBranch,
		"service_connection_id": (*buildDefiniton.Repository.Properties)["connectedServiceId"],
	}}
}

func expandBuildDefinition(d *schema.ResourceData) (*build.BuildDefinition, string, error) {
	projectID := d.Get("project_id").(string)
	repositories := d.Get("repository").(*schema.Set).List()

	variableGroupsInterface := d.Get("variable_groups").(*schema.Set).List()
	variableGroups := make([]build.VariableGroup, len(variableGroupsInterface))

	for i, variableGroup := range variableGroupsInterface {
		variableGroups[i] = *buildVariableGroup(variableGroup.(int))
	}

	// Note: If configured, this will be of length 1 based on the schema definition above.
	if len(repositories) != 1 {
		return nil, "", fmt.Errorf("Unexpectedly did not find repository metadata in the resource data")
	}

	repository := repositories[0].(map[string]interface{})

	repoName := repository["repo_name"].(string)
	repoType := repository["repo_type"].(string)
	repoURL := ""
	if strings.EqualFold(repoType, "github") {
		repoURL = fmt.Sprintf("https://github.com/%s.git", repoName)
	}

	// Look for the ID. This may not exist if we are within the context of a "create" operation,
	// so it is OK if it is missing.
	buildDefinitionID, err := strconv.Atoi(d.Id())
	var buildDefinitionReference *int
	if err == nil {
		buildDefinitionReference = &buildDefinitionID
	} else {
		buildDefinitionReference = nil
	}

	agentPoolName := d.Get("agent_pool_name").(string)
	buildDefinition := build.BuildDefinition{
		Id:       buildDefinitionReference,
		Name:     converter.String(d.Get("name").(string)),
		Path:     converter.String(d.Get("path").(string)),
		Revision: converter.Int(d.Get("revision").(int)),
		Repository: &build.BuildRepository{
			Url:           &repoURL,
			Id:            &repoName,
			Name:          &repoName,
			DefaultBranch: converter.String(repository["branch_name"].(string)),
			Type:          &repoType,
			Properties: &map[string]string{
				"connectedServiceId": repository["service_connection_id"].(string),
			},
		},
		Process: &build.YamlProcess{
			YamlFilename: converter.String(repository["yml_path"].(string)),
		},
		Queue: &build.AgentPoolQueue{
			Name: &agentPoolName,
			Pool: &build.TaskAgentPoolReference{
				Name: &agentPoolName,
			},
		},
		QueueStatus:    &build.DefinitionQueueStatusValues.Enabled,
		Type:           &build.DefinitionTypeValues.Build,
		Quality:        &build.DefinitionQualityValues.Definition,
		VariableGroups: &variableGroups,
	}

	return &buildDefinition, projectID, nil
}

func buildVariableGroup(id int) *build.VariableGroup {
	return &build.VariableGroup{
		Id: &id,
	}
}
