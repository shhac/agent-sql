import yaml from "js-yaml";

export const formatYaml = (data: unknown): string =>
	yaml.dump(data, {
		indent: 2,
		lineWidth: -1,
		noRefs: true,
		quotingType: '"',
		forceQuotes: false,
		sortKeys: false,
	});
