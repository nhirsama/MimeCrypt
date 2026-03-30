package providers

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

func ConfigurableSourceDrivers() []string {
	drivers := make([]string, 0, len(registeredDrivers))
	for name, driverImpl := range registeredDrivers {
		info := driverImpl.Info()
		if info.Source == nil {
			continue
		}
		if _, ok := driverImpl.(provider.SourceConfigurator); !ok {
			continue
		}
		drivers = append(drivers, normalizeDriver(name))
	}
	sort.Strings(drivers)
	return drivers
}

func ConfigureSourceConfig(driver string, source appconfig.Source, in io.Reader, out io.Writer) (appconfig.Source, error) {
	source = source.Configured()
	driverImpl, ok := LookupDriver(driver)
	if !ok {
		return appconfig.Source{}, fmt.Errorf("source driver 不支持: %s", driver)
	}
	if driverImpl.Info().Source == nil {
		return appconfig.Source{}, fmt.Errorf("source driver 不支持: %s", driver)
	}
	configurable, ok := driverImpl.(provider.SourceConfigurator)
	if !ok {
		return appconfig.Source{}, fmt.Errorf("source driver %s 未提供交互配置", driver)
	}
	configuredSource, err := configurable.ConfigureSource(source, in, out)
	if err != nil {
		return appconfig.Source{}, err
	}
	return configuredSource.Configured(), nil
}

func DescribeSourceConfig(source appconfig.Source) []string {
	source = source.Configured()
	driverImpl, ok := LookupDriver(source.Driver)
	if !ok {
		return []string{
			fmt.Sprintf("source=%s driver=%s mode=%s", strings.TrimSpace(source.Name), strings.TrimSpace(source.Driver), strings.TrimSpace(source.Mode)),
		}
	}
	configurable, ok := driverImpl.(provider.SourceConfigurator)
	if !ok {
		return []string{
			fmt.Sprintf("source=%s driver=%s mode=%s", strings.TrimSpace(source.Name), strings.TrimSpace(source.Driver), strings.TrimSpace(source.Mode)),
		}
	}
	return configurable.DescribeSource(source)
}
