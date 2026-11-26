& {
    $ErrorActionPreference = 'Stop'
    Add-Type -AssemblyName PresentationFramework

    $window = New-Object System.Windows.Window
    $window.Title = 'Computer MCP composited capture fixture'
    $window.Left = 100
    $window.Top = 100
    $window.Width = 640
    $window.Height = 320
    $window.ResizeMode = 'NoResize'
    $window.Topmost = $true

    $panel = New-Object System.Windows.Controls.DockPanel
    $red = New-Object System.Windows.Controls.Border
    $red.Width = 320
    $red.Background = [System.Windows.Media.Brushes]::Red
    $blue = New-Object System.Windows.Controls.Border
    $blue.Background = [System.Windows.Media.Brushes]::Blue
    $panel.Children.Add($red) | Out-Null
    $panel.Children.Add($blue) | Out-Null
    $window.Content = $panel

    $window.Add_ContentRendered({
        $helper = New-Object System.Windows.Interop.WindowInteropHelper $window
        $tier = [System.Windows.Media.RenderCapability]::Tier -shr 16
        [Console]::WriteLine(('{0} {1}' -f $helper.Handle.ToInt64(), $tier))
    })
    $window.ShowDialog() | Out-Null
}
