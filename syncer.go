package main

import (
	// "github.com/BurntSushi/toml"
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"sync"
)

type Syncer struct {
	config Config
}

func (syncer Syncer) syncPackages(origin string, channel string, upstream BldrApi, target BldrApi) bool {
	log.Info("Beginning the package sync process")

	upstreamPkgsChan := make(chan Packages)
	go func() {
		pkgs := upstream.listAllPackages(origin, channel)
		log.Info(fmt.Sprintf("Found %d packages on %s", len(pkgs.Data), upstream.Url))
		upstreamPkgsChan <- pkgs
	}()

	targetPkgsChan := make(chan Packages)
	go func() {
		targetPkgs := target.listAllPackages(origin, channel)
		log.Info(fmt.Sprintf("Found %d packages on %s", len(targetPkgs.Data), target.Url))
		targetPkgsChan <- targetPkgs
	}()

	upstreamPkgs := <-upstreamPkgsChan
	targetPkgs := <-targetPkgsChan

	// Good enough to figure out the difference before calculating deps
	pkgDatas := packageDifference(upstreamPkgs.Data, targetPkgs.Data)

	log.Info(fmt.Sprintf("Determining TDEPS for %d packages", len(pkgDatas)))

	// Currently adding multi-thread support for syncing packages pounds both upstream and target
	// APIs, typically resulting in Fatal API calls.

	// var wg sync.WaitGroup
	for j, p := range pkgDatas {
		// wg.Add(1)
		// go func(wg *sync.WaitGroup) {
		files := []string{}
		deps := upstream.fetchPackageDeps(p)
		log.Info(fmt.Sprintf("Determined deps %s", deps))
		for i, pkg := range deps {
			pkgName := fmt.Sprintf("%s/%s/%s/%s", pkg.Origin, pkg.Name, pkg.Version, pkg.Release)

			log.Info(fmt.Sprintf("Dependancy [%d/%d] %s", i+1, len(deps), pkgName))
			if !target.packageExists(pkg) {
				pack := upstream.fetchPackage(pkg)
				log.Info(fmt.Sprintf("Downloading package %s for target %s", pack.Name, pack.Target))
				file := upstream.downloadPackage(pack)
				files = append(files, file)
			} else {
				log.Info(fmt.Sprintf("Dependancy %s exists in target, skipping download", pkgName))
			}
		}

		log.Info(fmt.Sprintf("package [%d/%d]", j, len(pkgDatas)))
		pack := upstream.fetchPackage(p)
		pkgName := fmt.Sprintf("%s/%s/%s/%s", p.Origin, p.Name, p.Version, p.Release)
		log.Info(fmt.Sprintf("Downloading package %s for target %s", pack.Name, pack.Target))
		file := upstream.downloadPackage(pack)
		log.Info("Uploading package " + pkgName)
		packageUpload(target, file, "stable")
		files = append(files, file)

		for _, file := range files {
			log.Info("Cleaning up downloaded files")
			log.Debug("Removing file ", file)
			os.Remove(file)
		}
		// wg.Done()
		// }(&wg)
	}

	// wg.Wait()
	return true
}

func (syncer Syncer) syncKeys(origin string, upstream BldrApi, target BldrApi) bool {
	log.Info("Beginning the key sync process")
	upstreamKeys := upstream.fetchKeyPaths(origin)
	targetKeys := target.fetchKeyPaths(origin)
	keys := difference(upstreamKeys, targetKeys)
	log.Debug("Uploading diffed keys")
	log.Debug(keys)

	var wg sync.WaitGroup
	for _, key := range keys {
		// Sync Keys multi-threaded
		go func(wg *sync.WaitGroup) {
			data := upstream.fetchKeyData(key)
			log.Debug(data)
			fileName := key.Origin + "-" + key.Revision + ".pub"
			target.uploadOriginKey(fileName, data, key.Origin)

			wg.Done()
		}(&wg)
	}

	wg.Wait()
	return true
}

func (syncer Syncer) run() error {
	for _, origin := range syncer.config.Origins {
		syncer.syncKeys(origin.Name, syncer.config.Upstream, syncer.config.Target)
	}

	for _, origin := range syncer.config.Origins {
		for _, channel := range origin.Channels {
			log.Info(fmt.Sprintf("Syncing packages for %s on channel %s", origin.Name, channel))
			syncer.syncPackages(origin.Name, channel, syncer.config.Upstream, syncer.config.Target)
		}
	}

	return nil
}
