param([Parameter(Mandatory = $true)][string]$OutFile)

# Read only, selected benchmark fields. Never export host/user IDs or credentials.
$ErrorActionPreference = 'Stop'
$benchFullPath = [System.IO.Path]::GetFullPath($OutFile)
if ([System.IO.Path]::GetPathRoot($benchFullPath) -ne 'D:\') {
    throw 'Machine evidence must stay on D:.'
}
$benchResults = [ordered]@{}
$benchUtf8 = [System.Text.UTF8Encoding]::new($false)

function Save-BenchSection {
    param([string]$Name, [string]$Source, [scriptblock]$Read)
    $benchStarted = [DateTime]::UtcNow.ToString('o')
    try {
        $benchValues = @(& $Read)
        $benchResults[$Name] = [ordered]@{
            status = $(if ($benchValues.Count) { 'ok' } else { 'empty' })
            source = $Source
            started_utc = $benchStarted
            finished_utc = [DateTime]::UtcNow.ToString('o')
            data = $benchValues
            missing_reason = $null
        }
    } catch {
        $benchResults[$Name] = [ordered]@{
            status = 'unavailable'
            source = $Source
            started_utc = $benchStarted
            finished_utc = [DateTime]::UtcNow.ToString('o')
            data = $null
            missing_reason = $_.Exception.GetType().FullName
        }
    }
    # Retain completed sections if a later provider hangs and the owner times out.
    [System.IO.File]::WriteAllText($benchFullPath, ($benchResults | ConvertTo-Json -Depth 10), $benchUtf8)
}

Save-BenchSection 'system' 'CIM:Win32_ComputerSystem' {
    Get-CimInstance Win32_ComputerSystem -OperationTimeoutSec 5 |
        Select-Object Manufacturer, Model, SystemType, PCSystemType, TotalPhysicalMemory,
            NumberOfProcessors, NumberOfLogicalProcessors, HypervisorPresent, AutomaticManagedPagefile
}
Save-BenchSection 'os' 'CIM:Win32_OperatingSystem' {
    Get-CimInstance Win32_OperatingSystem -OperationTimeoutSec 5 |
        Select-Object Caption, Version, BuildNumber, OSArchitecture, OSLanguage,
            @{n='LastBootUpTimeUtc';e={$_.LastBootUpTime.ToUniversalTime().ToString('o')}},
            TotalVisibleMemorySize, FreePhysicalMemory, TotalVirtualMemorySize, FreeVirtualMemory
}
Save-BenchSection 'os_build' 'Registry:Windows NT/CurrentVersion' {
    Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' |
        Select-Object DisplayVersion, CurrentBuild, UBR, BuildLabEx, EditionID, InstallationType
}
Save-BenchSection 'processors' 'CIM:Win32_Processor' {
    Get-CimInstance Win32_Processor -OperationTimeoutSec 5 |
        Select-Object Name, Manufacturer, SocketDesignation, Architecture, AddressWidth,
            NumberOfCores, NumberOfEnabledCore, NumberOfLogicalProcessors, MaxClockSpeed,
            CurrentClockSpeed, L2CacheSize, L3CacheSize, VirtualizationFirmwareEnabled,
            VMMonitorModeExtensions, SecondLevelAddressTranslationExtensions
}
Save-BenchSection 'memory_modules' 'CIM:Win32_PhysicalMemory' {
    Get-CimInstance Win32_PhysicalMemory -OperationTimeoutSec 5 |
        Select-Object DeviceLocator, BankLabel, Capacity, Speed, ConfiguredClockSpeed,
            Manufacturer, PartNumber, SMBIOSMemoryType, FormFactor, DataWidth, TotalWidth
}
Save-BenchSection 'memory_arrays' 'CIM:Win32_PhysicalMemoryArray' {
    Get-CimInstance Win32_PhysicalMemoryArray -OperationTimeoutSec 5 |
        Select-Object MemoryDevices, MaxCapacity, MaxCapacityEx, MemoryErrorCorrection
}
Save-BenchSection 'pagefiles' 'CIM:Win32_PageFileUsage' {
    Get-CimInstance Win32_PageFileUsage -OperationTimeoutSec 5 |
        Select-Object Name, AllocatedBaseSize, CurrentUsage, PeakUsage, TempPageFile
}
Save-BenchSection 'bios' 'CIM:Win32_BIOS' {
    Get-CimInstance Win32_BIOS -OperationTimeoutSec 5 |
        Select-Object Manufacturer, SMBIOSBIOSVersion, SMBIOSMajorVersion, SMBIOSMinorVersion,
            @{n='ReleaseDateUtc';e={if ($_.ReleaseDate) {$_.ReleaseDate.ToUniversalTime().ToString('o')}}}
}
Save-BenchSection 'motherboard' 'CIM:Win32_BaseBoard' {
    Get-CimInstance Win32_BaseBoard -OperationTimeoutSec 5 |
        Select-Object Manufacturer, Product, Version
}
Save-BenchSection 'video_controllers' 'CIM:Win32_VideoController' {
    Get-CimInstance Win32_VideoController -OperationTimeoutSec 5 |
        Select-Object Name, VideoProcessor, AdapterRAM, DriverVersion,
            @{n='DriverDateUtc';e={if ($_.DriverDate) {$_.DriverDate.ToUniversalTime().ToString('o')}}},
            Status, ConfigManagerErrorCode
}
Save-BenchSection 'physical_disks' 'Storage:Get-PhysicalDisk' {
    Get-PhysicalDisk | Select-Object FriendlyName, MediaType, BusType, Size,
        HealthStatus, OperationalStatus, SpindleSpeed, FirmwareVersion
}
Save-BenchSection 'disks' 'Storage:Get-Disk' {
    Get-Disk | Select-Object Number, FriendlyName, BusType, Size, PartitionStyle,
        LogicalSectorSize, PhysicalSectorSize, IsBoot, IsSystem, IsOffline, IsReadOnly
}
Save-BenchSection 'volumes' 'CIM:Win32_LogicalDisk:DriveType=3' {
    Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' -OperationTimeoutSec 5 |
        Select-Object DeviceID, FileSystem, Size, FreeSpace, Compressed
}
Save-BenchSection 'batteries' 'CIM:Win32_Battery' {
    Get-CimInstance Win32_Battery -OperationTimeoutSec 5 |
        Select-Object Name, BatteryStatus, EstimatedChargeRemaining, EstimatedRunTime,
            DesignCapacity, FullChargeCapacity, DesignVoltage
}
Save-BenchSection 'network_links' 'CIM:Win32_NetworkAdapter:PhysicalAdapter=True' {
    Get-CimInstance Win32_NetworkAdapter -Filter 'PhysicalAdapter=True' -OperationTimeoutSec 5 |
        Select-Object Name, AdapterType, Speed, NetConnectionStatus, NetEnabled
}
Save-BenchSection 'thermal_zones' 'CIM:root/wmi:MSAcpi_ThermalZoneTemperature' {
    Get-CimInstance -Namespace root/wmi -ClassName MSAcpi_ThermalZoneTemperature -OperationTimeoutSec 5 |
        Select-Object CurrentTemperature, CriticalTripPoint, PassiveTripPoint
}
Save-BenchSection 'device_security' 'CIM:root/Microsoft/Windows/DeviceGuard:Win32_DeviceGuard' {
    Get-CimInstance -Namespace root/Microsoft/Windows/DeviceGuard -ClassName Win32_DeviceGuard -OperationTimeoutSec 5 |
        Select-Object VirtualizationBasedSecurityStatus, SecurityServicesConfigured, SecurityServicesRunning
}
Save-BenchSection 'secure_boot' 'Registry:SecureBoot/State' {
    Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\SecureBoot\State' |
        Select-Object UEFISecureBootEnabled
}
