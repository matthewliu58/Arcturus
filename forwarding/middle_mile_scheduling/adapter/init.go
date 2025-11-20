package adapter

import (
	log "github.com/sirupsen/logrus"

	"forwarding/middle_mile_scheduling/common"
)

// init automatically registers available algorithm adapters
func init() {
	// Register K-Shortest adapter with default k=3
	ksAdapter := NewKShortestAdapter(3)
	if err := common.RegisterGlobal("k_shortest", ksAdapter); err != nil {
		log.Warnf("Failed to register k_shortest adapter: %v", err)
	} else {
		log.Infof("Successfully registered k_shortest adapter")
	}

	// Register Carousel-Greedy adapter
	carouselAdapter := NewCarouselAdapter()

	// Register carousel_greedy adapter
	if err := common.RegisterGlobal("carousel_greedy", carouselAdapter); err != nil {
		log.Debugf("carousel_greedy already registered or naming conflict: %v", err)
	} else {
		log.Infof("Successfully registered carousel_greedy adapter")
	}

	log.Infof("Available routing algorithms: %v", common.ListGlobal())
}
