package processmanager

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/lavanet/lava/protocol/statetracker"
	"github.com/lavanet/lava/utils"
	protocoltypes "github.com/lavanet/lava/x/protocol/types"
)

type VersionMonitor struct {
	BinaryPath            string
	LavavisorPath         string
	lastKnownVersion      *protocoltypes.Version
	processes             []string
	autoDownload          bool
	protocolBinaryFetcher *ProtocolBinaryFetcher
	protocolBinaryLinker  *ProtocolBinaryLinker
	lock                  sync.Mutex
	LaunchedServices      bool // indicates whether version was matching or not so we can decide wether to launch services
}

func NewVersionMonitor(initVersion string, lavavisorPath string, processes []string, autoDownload bool) *VersionMonitor {
	var binaryPath string
	if initVersion != "" { // handle case if not found valid lavap at all
		versionDir := filepath.Join(lavavisorPath, "upgrades", "v"+initVersion)
		binaryPath = filepath.Join(versionDir, "lavap")
	}
	fetcher := &ProtocolBinaryFetcher{
		lavavisorPath: lavavisorPath,
	}
	return &VersionMonitor{
		BinaryPath:            binaryPath,
		LavavisorPath:         lavavisorPath,
		processes:             processes,
		autoDownload:          autoDownload,
		protocolBinaryFetcher: fetcher,
		protocolBinaryLinker:  &ProtocolBinaryLinker{Fetcher: fetcher},
		lock:                  sync.Mutex{},
	}
}

func (vm *VersionMonitor) handleUpdateTrigger() error {
	// set latest known version to incoming.
	utils.LavaFormatInfo("Update detected. Lavavisor starting the auto-upgrade...")
	// 1. check lavavisor directory first and attempt to fetch new binary from there
	versionDir := filepath.Join(vm.LavavisorPath, "upgrades", "v"+vm.lastKnownVersion.ProviderTarget)
	binaryPath := filepath.Join(versionDir, "lavap")
	vm.BinaryPath = binaryPath // updating new binary path for validating new binary

	// fetcher
	_, err := vm.protocolBinaryFetcher.FetchProtocolBinary(vm.autoDownload, vm.lastKnownVersion)
	if err != nil {
		return utils.LavaFormatError("Lavavisor was not able to fetch updated version. Skipping.", err, utils.Attribute{Key: "Version", Value: vm.lastKnownVersion.ProviderTarget})
	}
	return vm.createLinkAndRestartServices()
}

// create a link for lavap from the binary path and restart the services
func (vm *VersionMonitor) createLinkAndRestartServices() error {
	// linker
	err := vm.protocolBinaryLinker.CreateLink(vm.BinaryPath)
	if err != nil {
		return utils.LavaFormatError("Lavavisor was not able to create link to the binaries. Skipping.", err, utils.Attribute{Key: "Version", Value: vm.lastKnownVersion.ProviderTarget})
	}

	lavavisorServicesDir := vm.LavavisorPath + "/services/"
	if _, err := os.Stat(lavavisorServicesDir); os.IsNotExist(err) {
		return utils.LavaFormatError("Directory does not exist. Skipping.", nil, utils.Attribute{Key: "lavavisorServicesDir", Value: lavavisorServicesDir})
	}

	// First reload the daemon.
	err = ReloadDaemon()
	if err != nil {
		utils.LavaFormatError("Failed reloading daemon", err)
	}

	// now start all services
	var wg sync.WaitGroup
	for _, process := range vm.processes {
		wg.Add(1)
		go func(process string) {
			defer wg.Done() // Decrement the WaitGroup when done
			utils.LavaFormatInfo("Restarting process", utils.Attribute{Key: "Process", Value: process})
			err := StartProcess(process)
			if err != nil {
				utils.LavaFormatError("Failed starting process", err, utils.Attribute{Key: "Process", Value: process})
			}
			utils.LavaFormatInfo("Finished restarting process successfully", utils.Attribute{Key: "Process", Value: process})
		}(process)
	}
	// Wait for all Goroutines to finish
	wg.Wait()
	vm.LaunchedServices = true
	utils.LavaFormatInfo("Lavavisor successfully updated protocol version!", utils.Attribute{Key: "Upgraded version:", Value: vm.lastKnownVersion.ProviderTarget})
	return nil
}

func (vm *VersionMonitor) validateLinkPointsToTheRightTarget() error {
	lavapPath, err := vm.protocolBinaryLinker.FindLavaProtocolPath(vm.BinaryPath)
	if err != nil {
		return utils.LavaFormatError("Failed searching for lavap path, failed to validate link exists for lavap latest version", err)
	}
	var createLink bool
	_, err = os.Stat(lavapPath)
	if err != nil {
		// failed to validate lavap path.
		utils.LavaFormatDebug("lavap link path was not found attempting to create link", utils.Attribute{Key: "lavap_path", Value: lavapPath})
		createLink = true
	} else {
		utils.LavaFormatDebug("lavap link path found, validating linked binary is the right one", utils.Attribute{Key: "lavap_path", Value: lavapPath})
		// read link
		targetPath, err := os.Readlink(lavapPath)
		if err != nil {
			utils.LavaFormatInfo("failed reading link from lavap path", utils.Attribute{Key: "error", Value: err})
			createLink = true
		} else if targetPath != vm.BinaryPath {
			// utils.LavaFormatDebug("target validation", utils.Attribute{Key: "targetPath", Value: targetPath})
			utils.LavaFormatInfo("lavap link was pointing to the wrong binary. removing and creating a new link")
			createLink = true
		}
	}
	if createLink {
		utils.LavaFormatInfo("Attempting new link creation for lavap path", utils.Attribute{Key: "lavap", Value: lavapPath}, utils.Attribute{Key: "binary path", Value: vm.BinaryPath})
		err = vm.createLinkAndRestartServices()
	}
	return err
}

func (vm *VersionMonitor) ValidateProtocolVersion(incoming *statetracker.ProtocolVersionResponse) error {
	if !vm.lock.TryLock() { // if an upgrade is currently ongoing we don't need to check versions. just wait for the flow to end.
		utils.LavaFormatDebug("ValidateProtocolVersion is locked, assuming upgrade is ongoing")
		return nil
	}
	defer vm.lock.Unlock()
	currentBinaryVersion, _ := GetBinaryVersion(vm.BinaryPath)
	vm.lastKnownVersion = incoming.Version

	if currentBinaryVersion == "" || ValidateMismatch(incoming.Version, currentBinaryVersion) {
		utils.LavaFormatInfo("New version detected", utils.Attribute{Key: "incoming", Value: incoming})
		utils.LavaFormatInfo("Started Version Upgrade flow")
		err := vm.handleUpdateTrigger()
		if err != nil {
			utils.LavaFormatInfo("protocol update failed, lavavisor will continue trying to upgrade version every block until it succeeds")
		}
		return err
	} else if currentBinaryVersion != "" { // in case we have the latest version already installed we need to validate a few things.
		err := vm.validateLinkPointsToTheRightTarget()
		if err != nil {
			return utils.LavaFormatError("Failed to validateLinkPointsToTheRightTarget", err)
		}
	}

	// version is ok.
	utils.LavaFormatInfo("Validated protocol version",
		utils.Attribute{Key: "current_binary", Value: currentBinaryVersion},
		utils.Attribute{Key: "version_min", Value: incoming.Version.ProviderMin},
		utils.Attribute{Key: "version_target", Value: incoming.Version.ProviderTarget},
		utils.Attribute{Key: "lava_block_number", Value: incoming.BlockNumber})
	return nil
}
