package gaad

type SingleChannelElement = single_channel_element
type ChannelPairElement = channel_pair_element
type IndividualChannelStream = individual_channel_stream
type ICSInfo = ics_info
type PulseData = pulse_data
type ScaleFactorData = scale_factor_data
type SectionData = section_data
type SpectralData = spectral_data
type SpectralHuffmanCode = spectral_huffman_code
type TNSData = tns_data

func (info *ICSInfo) NumWindows() int {
	if info == nil {
		return 0
	}
	return int(info.num_windows)
}

func (info *ICSInfo) NumWindowGroups() int {
	if info == nil {
		return 0
	}
	return int(info.num_window_groups)
}

func (info *ICSInfo) WindowGroupLength() []uint8 {
	if info == nil {
		return nil
	}
	return info.window_group_length
}

func (info *ICSInfo) SectSFBOffset() [][]uint16 {
	if info == nil {
		return nil
	}
	return info.sect_sfb_offset
}

func (info *ICSInfo) SWBOffset() []uint16 {
	if info == nil {
		return nil
	}
	return info.swb_offset
}

func (info *ICSInfo) SFBCB() [][]uint8 {
	if info == nil {
		return nil
	}
	return info.sfb_cb
}
