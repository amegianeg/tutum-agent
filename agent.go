package main

import (
	"os"
	"path"
	"runtime"
	"syscall"
	"time"

	. "github.com/tutumcloud/tutum-agent/agent"
	"github.com/tutumcloud/tutum-agent/utils"
)

func init() {
	runtime.GOMAXPROCS(4)
}

func main() {
	dockerBinPath := path.Join(DockerDir, DockerBinaryName)
	dockerNewBinPath := path.Join(DockerDir, DockerNewBinaryName)
	dockerNewBinSigPath := path.Join(DockerDir, DockerNewBinarySigName)
	configFilePath := path.Join(TutumHome, ConfigFileName)
	keyFilePath := path.Join(TutumHome, KeyFileName)
	certFilePath := path.Join(TutumHome, CertFileName)
	caFilePath := path.Join(TutumHome, CAFileName)
	ngrokPath := path.Join(DockerDir, NgrokBinaryName)
	ngrokLogPath := path.Join(LogDir, NgrokLogName)
	ngrokConfPath := path.Join(TutumHome, NgrokConfName)

	_ = os.MkdirAll(TutumHome, 0755)
	_ = os.MkdirAll(DockerDir, 0755)
	_ = os.MkdirAll(LogDir, 0755)

	ParseFlag()
	SetLogger(path.Join(LogDir, TutumLogFileName))
	PrepareFiles(configFilePath, dockerBinPath, keyFilePath, certFilePath)
	SetConfigFile(configFilePath)

	regUrl := utils.JoinURL(Conf.TutumHost, RegEndpoint)
	if Conf.TutumUUID == "" {
		os.RemoveAll(keyFilePath)
		os.RemoveAll(certFilePath)
		os.RemoveAll(caFilePath)

		if !*FlagStandalone {
			Logger.Printf("Registering in Tutum via POST: %s...\n", regUrl)
			PostToTutum(regUrl, caFilePath, configFilePath)
		}
	}

	if *FlagStandalone {
		commonName := Conf.CertCommonName
		if commonName == "" {
			commonName = "*"
		}
		CreateCerts(keyFilePath, certFilePath, commonName)
	} else {
		CreateCerts(keyFilePath, certFilePath, Conf.CertCommonName)
	}

	if !*FlagStandalone {
		Logger.Printf("Registering in Tutum via PATCH: %s...\n",
			regUrl+Conf.TutumUUID)
		err := PatchToTutum(regUrl, caFilePath, certFilePath, configFilePath)
		if err != nil {
			Logger.Printf("TutumUUID (%s) is invalid, trying to allocate a new one...\n", Conf.TutumUUID)
			Conf.TutumUUID = ""
			SaveConf(configFilePath, Conf)

			os.RemoveAll(keyFilePath)
			os.RemoveAll(certFilePath)
			os.RemoveAll(caFilePath)

			Logger.Printf("Registering in Tutum via POST: %s...\n", regUrl)
			PostToTutum(regUrl, caFilePath, configFilePath)

			CreateCerts(keyFilePath, certFilePath, Conf.CertCommonName)

			Logger.Printf("Registering in Tutum via PATCH: %s...\n",
				regUrl+Conf.TutumUUID)
			if err = PatchToTutum(regUrl, caFilePath, certFilePath, configFilePath); err != nil {
				SendError(err, "Registion HTTP error", nil)
			}
		}
	}
	DownloadDocker(DockerBinaryURL, dockerBinPath)
	HandleSig()
	syscall.Setpriority(syscall.PRIO_PROCESS, os.Getpid(), RenicePriority)

	Logger.Println("Starting docker daemon...")
	StartDocker(dockerBinPath, keyFilePath, certFilePath, caFilePath)

	if !*FlagStandalone {
		if NgrokBinaryURL != "" {
			Logger.Println("Downloading NAT tunnel module...")
			DownloadNgrok(NgrokBinaryURL, ngrokPath)
		}
		if *FlagSkipNatTunnel {
			Logger.Println("Skip NAT tunnel")
		} else {
			Logger.Println("Loading NAT tunnel module...")
			go NatTunnel(regUrl, ngrokPath, ngrokLogPath, ngrokConfPath, NodePublicIp)
		}
	}

	if !*FlagStandalone {
		Logger.Println("Verifying the registration with Tutum...")
		go VerifyRegistration(regUrl)
	}

	Logger.Println("Docker server started. Entering maintenance loop")
	for {
		time.Sleep(HeartBeatInterval * time.Second)
		UpdateDocker(dockerBinPath, dockerNewBinPath, dockerNewBinSigPath, keyFilePath, certFilePath, caFilePath)

		// try to restart docker daemon if it dies somehow
		if DockerProcess == nil {
			time.Sleep(HeartBeatInterval * time.Second)
			if DockerProcess == nil && ScheduleToTerminateDocker == false {
				Logger.Println("Respawning docker daemon")
				StartDocker(dockerBinPath, keyFilePath, certFilePath, caFilePath)
			}
		}
	}
}

func PrepareFiles(configFilePath, dockerBinPath, keyFilePath, certFilePath string) {
	Logger.Println("Checking if config file exists...")
	if !utils.FileExist(configFilePath) {
		LoadDefaultConf()
		if err := SaveConf(configFilePath, Conf); err != nil {
			SendError(err, "Failed to save config to the conf file", nil)
			Logger.Fatalln(err)
		}
	}

	Logger.Println("Loading Configuration file...")
	conf, err := LoadConf(configFilePath)
	if err != nil {
		SendError(err, "Failed to load configuration file", nil)
		Logger.Fatalln("Failed to load configuration file:", err)
	} else {
		Conf = *conf
	}

	if *FlagDockerHost != "" {
		Logger.Printf("Override 'DockerHost' from command line flag: %s\n", *FlagDockerHost)
		Conf.DockerHost = *FlagDockerHost
	}
	if *FlagTutumHost != "" {
		Logger.Printf("Override 'TutumHost' from command line flag: %s\n", *FlagTutumHost)
		Conf.TutumHost = *FlagTutumHost
	}
	if *FlagTutumToken != "" {
		Logger.Printf("Override 'TutumToken' from command line flag: %s\n", *FlagTutumToken)
		Conf.TutumToken = *FlagTutumToken
	}
	if *FlagTutumUUID != "" {
		Logger.Printf("Override 'TutumUUID' from command line flag: %s\n", *FlagTutumUUID)
		Conf.TutumUUID = *FlagTutumUUID
	}
}
