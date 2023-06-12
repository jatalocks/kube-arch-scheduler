package archfilter

import (
	"context"
	"errors"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/cache"
)

const (
	// Name is plugin name
	Name = "archfilter"
)

type ImageArch struct {
	Image        string
	Architecture string
}

var _ framework.FilterPlugin = &ArchFilter{}

func cacheKeyFunc(obj interface{}) (string, error) {
	return obj.(ImageArch).Image, nil
}

var cacheStore = cache.NewTTLStore(cacheKeyFunc, time.Duration(10)*time.Minute)

func AddToCache(cacheStore cache.Store, object ImageArch) error {
	return cacheStore.Add(object)
}

func FetchFromCache(cacheStore cache.Store, key string) (ImageArch, error) {
	obj, exists, err := cacheStore.GetByKey(key)
	if err != nil {
		// klog.Errorf("failed to add key value to cache error", err)
		return ImageArch{}, err
	}
	if !exists {
		// klog.Errorf("object does not exist in the cache")
		err = errors.New("object does not exist in the cache")
		return ImageArch{}, err
	}
	return obj.(ImageArch), nil
}

func DeleteFromCache(cacheStore cache.Store, object string) error {
	return cacheStore.Delete(object)
}

// var _ framework.PreBindPlugin = &ArchFilter{}

type ArchFilter struct {
	handle framework.Handle
}

func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &ArchFilter{
		handle: handle,
	}, nil
}

func (s *ArchFilter) Name() string {
	return Name
}

func GetPodArchitectures(pod *v1.Pod) ([]string, error) {
	architectures := make([]string, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		val, err := FetchFromCache(cacheStore, container.Image)
		// If the key exists
		if err == nil {
			architectures = append(architectures, val.Architecture)
		} else {
			klog.V(2).Info(container.Image)
			ref, err := name.ParseReference(container.Image)
			if err != nil {
				return architectures, err
			}
			img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
			if err != nil {
				return architectures, err
			}
			manifest, err := img.ConfigFile()
			if err != nil {
				return architectures, err
			}
			AddToCache(cacheStore, ImageArch{Image: container.Image, Architecture: manifest.Architecture})
			architectures = append(architectures, manifest.Architecture)
		}
	}

	return architectures, nil
}

func (s *ArchFilter) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, node *framework.NodeInfo) *framework.Status {
	nodeArch := node.Node().Status.NodeInfo.Architecture
	klog.V(2).Infof("filter pod: %v, %v", pod.Name, nodeArch)
	podArchitectures, err := GetPodArchitectures(pod)
	if err != nil {
		klog.V(2).ErrorS(err, "failed to get image architectures")
		return framework.NewStatus(framework.Error, "Failed to get pod architectures")
	}
	for _, arch := range podArchitectures {
		if nodeArch != arch {
			return framework.NewStatus(framework.Unschedulable, "Incompatible node architecture found", nodeArch, arch)
		}
	}

	return framework.NewStatus(framework.Success, "Node with compatible architecture found", nodeArch)
}

// // GetDigest return the docker digest of given image name
// func GetDigest(ctx context.Context, name string) (string, error) {
// 	if digestCache[name] != "" {
// 		return digestCache[name], nil
// 	}
// 	ref, err := docker.ParseReference("//" + name)
// 	if err != nil {
// 		return "", err
// 	}
// 	img, err := ref.NewImage(ctx, nil)
// 	if err != nil {
// 		return "", err
// 	}
// 	defer func() {
// 		if err := img.Close(); err != nil {
// 			log.Print(err)
// 		}
// 	}()
// 	b, _, err := img.Manifest(ctx)
// 	if err != nil {
// 		return "", err
// 	}
// 	digest, err := manifest.Digest(b)
// 	if err != nil {
// 		return "", err
// 	}
// 	digeststr := string(digest)
// 	digestCache[name] = digeststr
// 	return digeststr, nil
// }
