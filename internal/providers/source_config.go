package providers

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"mimecrypt/internal/appconfig"
)

func ConfigurableSourceDrivers() []string {
	drivers := make([]string, 0, len(driverRegistrations))
	for name, registration := range driverRegistrations {
		if registration.Spec.Source == nil || registration.ConfigureSource == nil {
			continue
		}
		drivers = append(drivers, normalizeDriver(name))
	}
	sort.Strings(drivers)
	return drivers
}

func ConfigureSourceConfig(driver string, source appconfig.Source, in io.Reader, out io.Writer) (appconfig.Source, error) {
	registration, ok := lookupDriverRegistration(driver)
	if !ok || registration.Spec.Source == nil {
		return appconfig.Source{}, fmt.Errorf("source driver 不支持: %s", driver)
	}
	if registration.ConfigureSource == nil {
		return appconfig.Source{}, fmt.Errorf("source driver %s 未提供交互配置", driver)
	}
	return registration.ConfigureSource(source, in, out)
}

func DescribeSourceConfig(source appconfig.Source) []string {
	registration, ok := lookupDriverRegistration(source.Driver)
	if !ok || registration.DescribeSource == nil {
		return []string{
			fmt.Sprintf("source=%s driver=%s mode=%s", strings.TrimSpace(source.Name), strings.TrimSpace(source.Driver), strings.TrimSpace(source.Mode)),
		}
	}
	return registration.DescribeSource(source)
}
