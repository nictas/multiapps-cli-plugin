package commands_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	plugin_fakes "github.com/cloudfoundry/cli/plugin/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/SAP/cf-mta-plugin/clients/models"
	restfake "github.com/SAP/cf-mta-plugin/clients/restclient/fakes"
	slmpfake "github.com/SAP/cf-mta-plugin/clients/slmpclient/fakes"
	slppfake "github.com/SAP/cf-mta-plugin/clients/slppclient/fakes"
	"github.com/SAP/cf-mta-plugin/commands"
	cmd_fakes "github.com/SAP/cf-mta-plugin/commands/fakes"
	"github.com/SAP/cf-mta-plugin/testutil"
	"github.com/SAP/cf-mta-plugin/ui"
	"github.com/SAP/cf-mta-plugin/util"
)

var _ = Describe("DeployCommand", func() {
	Describe("Execute", func() {
		const org = "test-org"
		const space = "test-space"
		const user = "test-user"
		const testFilesLocation = "../test_resources/commands/"
		const testArchive = "mtaArchive.mtar"
		const mtaArchivePath = testFilesLocation + testArchive
		const extDescriptorPath = testFilesLocation + "extDescriptor.mtaext"

		var name string
		var cliConnection *plugin_fakes.FakeCliConnection
		var slmpClient *slmpfake.FakeSlmpClientOperations
		var slppClient *slppfake.FakeSlppClientOperations
		var restClient *restfake.FakeRestClientOperations
		var testClientFactory *commands.TestClientFactory
		var command *commands.DeployCommand
		var oc = testutil.NewUIOutputCapturer()
		var ex = testutil.NewUIExpector()

		var fullMtaArchivePath, _ = filepath.Abs(mtaArchivePath)
		var fullExtDescriptorPath, _ = filepath.Abs(extDescriptorPath)

		var getLinesForAbortingProcess = func() []string {
			return []string{
				"Aborting multi-target app operation with id test-process-id...\n",
				"OK\n",
			}
		}

		var getOutputLines = func(extDescriptor, processAborted bool) []string {
			lines := []string{}
			lines = append(lines,
				"Deploying multi-target app archive "+mtaArchivePath+" in org "+org+" / space "+space+" as "+user+"...\n")
			if processAborted {
				lines = append(lines,
					"Aborting multi-target app operation with id process-id...\n",
					"OK\n",
				)
			}
			lines = append(lines,
				"Uploading 1 files...\n",
				"  "+fullMtaArchivePath+"\n",
				"OK\n")
			if extDescriptor {
				lines = append(lines,
					"Uploading 1 files...\n",
					"  "+fullExtDescriptorPath+"\n",
					"OK\n")
			}
			lines = append(lines,
				"Starting deployment process...\n",
				"OK\n",
				"Monitoring process execution...\n",
				"Process finished.\n")
			return lines
		}

		var getProcessParameters = func(additional bool) map[string]string {
			params := map[string]string{
				"appArchiveId":   "mtaArchive.mtar",
				"targetPlatform": "test-org test-space",
				"failOnCrashed":  "false",
			}
			if additional {
				params["deleteServices"] = "true"
				params["keepFiles"] = "true"
				params["noStart"] = "true"
			}
			return params
		}

		var getFile = func(path string) (*os.File, *models.File) {
			file, _ := os.Open(path)
			digest, _ := util.ComputeFileChecksum(path, "MD5")
			f := testutil.GetFile("xs2-deploy", *file, strings.ToUpper(digest))
			return file, f
		}

		var expectProcessParameters = func(expectedParameters map[string]string, processParameters models.ProcessParameters) {
			for _, processParameter := range processParameters.Parameters {
				if expectedParameters[*processParameter.ID] != "" {
					Expect(processParameter.Value).To(Equal(expectedParameters[*processParameter.ID]))
				}
			}
		}

		BeforeEach(func() {
			ui.DisableTerminalOutput(true)
			name = command.GetPluginCommand().Name
			cliConnection = cmd_fakes.NewFakeCliConnectionBuilder().
				CurrentOrg("test-org-guid", org, nil).
				CurrentSpace("test-space-guid", space, nil).
				Username(user, nil).
				AccessToken("bearer test-token", nil).
				APIEndpoint("https://api.test.ondemand.com", nil).Build()
			mtaArchiveFile, mtaArchive := getFile(mtaArchivePath)
			defer mtaArchiveFile.Close()
			extDescriptorFile, extDescriptor := getFile(extDescriptorPath)
			defer extDescriptorFile.Close()
			slmpClient = slmpfake.NewFakeSlmpClientBuilder().
				GetMetadata(&testutil.SlmpMetadataResult, nil).
				GetService("xs2-deploy", testutil.GetService("xs2-deploy", "Deploy", []*models.Parameter{testutil.GetParameter("mtaArchiveId")}), nil).
				GetServiceFiles("xs2-deploy", testutil.FilesResult, nil).
				CreateServiceFile("xs2-deploy", mtaArchiveFile, testutil.GetFiles([]*models.File{mtaArchive}), nil).
				CreateServiceFile("xs2-deploy", extDescriptorFile, testutil.GetFiles([]*models.File{extDescriptor}), nil).
				CreateServiceProcess("", nil, &testutil.ProcessResult, nil).Build()
			slppClient = slppfake.NewFakeSlppClientBuilder().
				GetMetadata(&testutil.SlppMetadataResult, nil).
				GetTasklistTask(&testutil.TaskResult, nil).
				GetLogContent(testutil.LogID, testutil.LogContent, nil).Build()
			restClient = restfake.NewFakeRestClientBuilder().
				GetOperations(nil, nil, testutil.OperationsResult, nil).Build()
			testClientFactory = commands.NewTestClientFactory(slmpClient, slppClient, restClient)
			command = commands.NewDeployCommand()
			testTokenFactory := commands.NewTestTokenFactory(cliConnection)
			command.InitializeAll(name, cliConnection, testutil.NewCustomTransport(200, nil), nil, testClientFactory, testTokenFactory)
		})

		// unknown flag - error
		Context("with an unknown flag", func() {
			It("should print incorrect usage, call cf help, and exit with a non-zero status", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"x", "-l"}).ToInt()
				})
				ex.ExpectFailure(status, output, "Incorrect usage. Unknown or wrong flag.")
				Expect(cliConnection.CliCommandArgsForCall(0)).To(Equal([]string{"help", name}))
			})
		})

		// wrong arguments - error
		Context("with wrong arguments", func() {
			It("should print incorrect usage, call cf help, and exit with a non-zero status", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"x", "y", "z"}).ToInt()
				})
				ex.ExpectFailure(status, output, "Incorrect usage. Wrong arguments.")
				Expect(cliConnection.CliCommandArgsForCall(0)).To(Equal([]string{"help", name}))
			})
		})

		// no arguments - error
		Context("with no arguments", func() {
			It("should print incorrect usage, call cf help, and exit with a non-zero status", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{}).ToInt()
				})
				ex.ExpectFailure(status, output, "Incorrect usage. Missing positional argument 'MTA'.")
				Expect(cliConnection.CliCommandArgsForCall(0)).To(Equal([]string{"help", name}))
			})
		})

		// no MTA argument - error
		Context("with no MTA argument", func() {
			It("should print incorrect usage, call cf help, and exit with a non-zero status", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"-e", "test.mtaext"}).ToInt()
				})
				ex.ExpectFailure(status, output, "Incorrect usage. Missing positional argument 'MTA'.")
				Expect(cliConnection.CliCommandArgsForCall(0)).To(Equal([]string{"help", name}))
			})
		})

		// non-existing MTA archive - error
		Context("with a non-existing mta archive", func() {
			It("should print a file not found error and exit with a non-zero status", func() {
				const fileName = "non-existing-mtar.mtar"
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{fileName}).ToInt()
				})
				abs, _ := filepath.Abs(fileName)
				ex.ExpectFailureOnLine(status, output, "Could not find file "+abs, 1)
			})
		})

		// TODO: can't connect to backend - error

		// TODO: backend returns an an error response - error

		// existing MTA archive - success
		Context("with an existing mta archive", func() {
			It("should upload 1 file and start the deployment process", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{mtaArchivePath}).ToInt()
				})
				ex.ExpectSuccessWithOutput(status, output, getOutputLines(false, false))
				serviceID, process := slmpClient.CreateServiceProcessArgsForCall(0)
				Expect(serviceID).To(Equal("xs2-deploy"))
				expectProcessParameters(getProcessParameters(false), process.Parameters)
			})
		})

		// existing MTA archive and an extension descriptor - success
		Context("with an existing mta archive and an extension descriptor", func() {
			It("should upload 2 files and start the deployment process", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{mtaArchivePath, "-e", extDescriptorPath}).ToInt()
				})
				ex.ExpectSuccessWithOutput(status, output, getOutputLines(true, false))
				serviceID, process := slmpClient.CreateServiceProcessArgsForCall(0)
				Expect(serviceID).To(Equal("xs2-deploy"))
				expectProcessParameters(getProcessParameters(false), process.Parameters)
			})
		})

		// existing MTA archive and additional options - success
		Context("with an existing mta archive and some options", func() {
			It("should upload 1 file and start the deployment process", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{mtaArchivePath, "-f", "-delete-services", "-no-start", "-keep-files", "-do-not-fail-on-missing-permissions"}).ToInt()
				})
				ex.ExpectSuccessWithOutput(status, output, getOutputLines(false, false))
				serviceID, process := slmpClient.CreateServiceProcessArgsForCall(0)
				Expect(serviceID).To(Equal("xs2-deploy"))
				expectProcessParameters(getProcessParameters(true), process.Parameters)
			})
		})

		// non-existing ongoing operations - success
		Context("with correct mta id from archive and no ongoing operations", func() {
			It("should not try to abort confliction operations", func() {
				testClientFactory.RestClient = restfake.NewFakeRestClientBuilder().
					GetOperations(nil, nil, testutil.GetOperations([]*models.Operation{}), nil).Build()
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{mtaArchivePath}).ToInt()
				})
				ex.ExpectSuccessWithOutput(status, output, getOutputLines(false, false))
				serviceID, process := slmpClient.CreateServiceProcessArgsForCall(0)
				Expect(serviceID).To(Equal("xs2-deploy"))
				expectProcessParameters(getProcessParameters(false), process.Parameters)
			})
		})

		// existing ongoing operations and force option not supplied - success
		Context("with correct mta id from archive, with ongoing operations provided and no force option", func() {
			It("should not try to abort confliction operations", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{mtaArchivePath}).ToInt()
				})
				ex.ExpectSuccessWithOutput(status, output, getOutputLines(false, false))
				serviceID, process := slmpClient.CreateServiceProcessArgsForCall(0)
				Expect(serviceID).To(Equal("xs2-deploy"))
				expectProcessParameters(getProcessParameters(false), process.Parameters)
			})
		})

		// existing ongoing operations and force option supplied - success
		Context("with correct mta id from archive, with ongoing operations provided and with force option", func() {
			It("should try to abort confliction operations", func() {
				testClientFactory.RestClient = restfake.NewFakeRestClientBuilder().
					GetOperations(nil, nil, testutil.GetOperations([]*models.Operation{testutil.GetOperation("process-id", "test-space-guid", "test", "deploy", "SLP_TASK_STATE_ERROR", true)}), nil).Build()
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{mtaArchivePath, "-f"}).ToInt()
				})
				ex.ExpectSuccessWithOutput(status, output, getOutputLines(false, true))
				serviceID, process := slmpClient.CreateServiceProcessArgsForCall(0)
				Expect(serviceID).To(Equal("xs2-deploy"))
				expectProcessParameters(getProcessParameters(false), process.Parameters)
			})
		})
		Context("with an error returned from getting ongoing operations", func() {
			It("should display error and exit witn non-zero status", func() {
				testClientFactory.RestClient = restfake.NewFakeRestClientBuilder().
					GetOperations(nil, nil, testutil.GetOperations([]*models.Operation{}), fmt.Errorf("test-error-from backend")).Build()
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{mtaArchivePath}).ToInt()
				})
				ex.ExpectFailureOnLine(status, output, "Could not get ongoing operation", 1)
			})
		})

		Context("with non-valid operation id and action id provided", func() {
			It("should return error and exit with non-zero status", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"-i", "test", "-a", "abort"}).ToInt()
				})
				ex.ExpectFailureOnLine(status, output, "Multi-target app operation with id test not found", 0)
			})
		})
		Context("with valid operation id and non-valid action id provided", func() {
			It("should return error and exit with non-zero status", func() {
				testClientFactory.RestClient = restfake.NewFakeRestClientBuilder().
					GetOperations(nil, nil, testutil.GetOperations([]*models.Operation{
						testutil.GetOperation("test-process-id", "test-space", "test-mta-id", "deploy", "SLP_TASK_STATE_ERROR", true),
					}), nil).Build()
				testClientFactory.SlppClient = slppfake.NewFakeSlppClientBuilder().
					GetMetadata(&testutil.SlppMetadataResult, nil).Build()
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"-i", "test-process-id", "-a", "test"}).ToInt()
				})
				ex.ExpectFailureOnLine(status, output, "Invalid action test", 0)
			})
		})
		Context("with valid operation id and no action id provided", func() {
			It("should return error and exit with non-zero status", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"-i", "test-process-id"}).ToInt()
				})
				ex.ExpectFailureOnLine(status, output, "All the a i options should be specified together", 0)
			})
		})

		Context("with valid action id and no operation id provided", func() {
			It("should return error and exit with non-zero status", func() {
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"-a", "abort"}).ToInt()
				})
				ex.ExpectFailureOnLine(status, output, "All the a i options should be specified together", 0)
			})
		})

		Context("with valid operation id and valid action id provided", func() {
			It("should execute action on the process specified with process id and exit with zero status", func() {
				testClientFactory.RestClient = restfake.NewFakeRestClientBuilder().
					GetOperations(nil, nil, testutil.GetOperations([]*models.Operation{
						testutil.GetOperation("test-process-id", "test-space", "test-mta-id", "deploy", "SLP_TASK_STATE_ERROR", true),
					}), nil).Build()
				testClientFactory.SlppClient = slppfake.NewFakeSlppClientBuilder().
					GetMetadata(&testutil.SlppMetadataResult, nil).
					ExecuteAction("test", nil).Build()
				output, status := oc.CaptureOutputAndStatus(func() int {
					return command.Execute([]string{"-i", "test-process-id", "-a", "abort"}).ToInt()
				})
				ex.ExpectSuccessWithOutput(status, output, getLinesForAbortingProcess())
			})
		})
	})
})