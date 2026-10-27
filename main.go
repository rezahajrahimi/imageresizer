package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/disintegration/imaging"
)

type InteractiveCanvas struct {
	widget.BaseWidget
	state        *AppState
	dragStartPos fyne.Position
	dragCurrent  fyne.Position
	isDragging   bool
}

func newInteractiveCanvas(s *AppState) *InteractiveCanvas {
	c := &InteractiveCanvas{state: s}
	c.ExtendBaseWidget(c)
	return c
}

func (c *InteractiveCanvas) CreateRenderer() fyne.WidgetRenderer {
	return &canvasRenderer{
		interactive: c,
	}
}

type canvasRenderer struct {
	interactive *InteractiveCanvas
}

func (r *canvasRenderer) Layout(size fyne.Size) {
	if r.interactive.state.previewImg != nil {
		r.interactive.state.previewImg.Resize(size)
		r.interactive.state.previewImg.Move(fyne.NewPos(0, 0))
	}
	if r.interactive.state.cropOverlay != nil {
		r.interactive.updateCropOverlayGeometry(size)
	}
}

func (r *canvasRenderer) MinSize() fyne.Size {
	return fyne.NewSize(360, 360)
}

func (r *canvasRenderer) Refresh() {
	if r.interactive.state.previewImg != nil {
		r.interactive.state.previewImg.Refresh()
	}
	if r.interactive.state.cropOverlay != nil {
		r.interactive.updateCropOverlayGeometry(r.interactive.Size())
		r.interactive.state.cropOverlay.Refresh()
	}
}

func (r *canvasRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{}
	if r.interactive.state.previewImg != nil {
		objs = append(objs, r.interactive.state.previewImg)
	}
	if r.interactive.state.cropOverlay != nil {
		objs = append(objs, r.interactive.state.cropOverlay)
	}
	return objs
}

func (r *canvasRenderer) Destroy() {}

func (c *InteractiveCanvas) getImageRenderRect(containerSize fyne.Size) (float32, float32, float32, float32) {
	if c.state.loadedImg == nil || c.state.origWidth == 0 || c.state.origHeight == 0 {
		return 0, 0, containerSize.Width, containerSize.Height
	}

	imgW := float32(c.state.origWidth)
	imgH := float32(c.state.origHeight)
	viewW := containerSize.Width
	viewH := containerSize.Height

	scaleX := viewW / imgW
	scaleY := viewH / imgH
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	drawW := imgW * scale
	drawH := imgH * scale
	drawX := (viewW - drawW) / 2
	drawY := (viewH - drawH) / 2

	return drawX, drawY, drawW, drawH
}

func (c *InteractiveCanvas) updateCropOverlayGeometry(containerSize fyne.Size) {
	if !c.state.cropActive || c.state.loadedImg == nil {
		c.state.cropOverlay.Hide()
		return
	}

	drawX, drawY, drawW, drawH := c.getImageRenderRect(containerSize)
	if drawW <= 0 || drawH <= 0 {
		c.state.cropOverlay.Hide()
		return
	}

	scale := drawW / float32(c.state.origWidth)

	rectX := drawX + float32(c.state.selX)*scale
	rectY := drawY + float32(c.state.selY)*scale
	rectW := float32(c.state.selW) * scale
	rectH := float32(c.state.selH) * scale

	c.state.cropOverlay.Move(fyne.NewPos(rectX, rectY))
	c.state.cropOverlay.Resize(fyne.NewSize(rectW, rectH))
	c.state.cropOverlay.Show()
}

func (c *InteractiveCanvas) Dragged(e *fyne.DragEvent) {
	if c.state.loadedImg == nil {
		return
	}
	if !c.isDragging {
		c.isDragging = true
		c.dragStartPos = e.Position.Subtract(e.Dragged)
	}
	c.dragCurrent = e.Position

	drawX, drawY, drawW, _ := c.getImageRenderRect(c.Size())
	scale := drawW / float32(c.state.origWidth)
	if scale <= 0 {
		return
	}

	x1 := (c.dragStartPos.X - drawX) / scale
	y1 := (c.dragStartPos.Y - drawY) / scale
	x2 := (c.dragCurrent.X - drawX) / scale
	y2 := (c.dragCurrent.Y - drawY) / scale

	minX := int(math.Min(float64(x1), float64(x2)))
	maxX := int(math.Max(float64(x1), float64(x2)))
	minY := int(math.Min(float64(y1), float64(y2)))
	maxY := int(math.Max(float64(y1), float64(y2)))

	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX > c.state.origWidth {
		maxX = c.state.origWidth
	}
	if maxY > c.state.origHeight {
		maxY = c.state.origHeight
	}

	w := maxX - minX
	h := maxY - minY

	c.state.selX = minX
	c.state.selY = minY
	c.state.selW = w
	c.state.selH = h
	c.state.cropActive = (w > 5 && h > 5)

	c.state.syncCropEntries()
	c.Refresh()
}

func (c *InteractiveCanvas) DragEnd() {
	c.isDragging = false
	c.Refresh()
}

func (c *InteractiveCanvas) Cursor() desktop.Cursor {
	return desktop.CrosshairCursor
}

type AppState struct {
	window          fyne.Window
	fileList        []string
	displayFileList []string
	searchQuery     string
	extFilter       string
	sortOrder       string
	currentFolder   string
	currentIndex    int

	originalPath string
	loadedImg    image.Image
	previewImg   *canvas.Image
	cropOverlay  *canvas.Rectangle
	interCanvas  *InteractiveCanvas

	fileListView *widget.List
	searchEntry  *widget.Entry
	filterSelect *widget.Select
	sortSelect   *widget.Select
	infoLabel    *widget.Label

	widthEntry  *widget.Entry
	heightEntry *widget.Entry
	lockAspect  *widget.Check

	cropX *widget.Entry
	cropY *widget.Entry
	cropW *widget.Entry
	cropH *widget.Entry

	selX       int
	selY       int
	selW       int
	selH       int
	cropActive bool

	qualitySlider *widget.Slider
	qualityLabel  *widget.Label
	formatSelect  *widget.Select

	origWidth  int
	origHeight int
	updating   bool
}

func main() {
	myApp := app.NewWithID("com.reza.imageresizer")
	w := myApp.NewWindow("Image Resizer & Studio")
	w.Resize(fyne.NewSize(1150, 750))

	state := &AppState{
		window:       w,
		currentIndex: -1,
	}

	state.buildUI()
	w.ShowAndRun()
}

func (s *AppState) buildUI() {
	s.infoLabel = widget.NewLabel("عکسی باز نشده است")
	s.infoLabel.Wrapping = fyne.TextWrapWord

	s.previewImg = canvas.NewImageFromImage(nil)
	s.previewImg.FillMode = canvas.ImageFillContain

	s.cropOverlay = canvas.NewRectangle(color.NRGBA{R: 255, G: 80, B: 80, A: 90})
	s.cropOverlay.StrokeColor = color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	s.cropOverlay.StrokeWidth = 2
	s.cropOverlay.Hide()

	s.interCanvas = newInteractiveCanvas(s)

	previewCard := widget.NewCard("پیش‌نمایش تعاملی (برای کراپ روی عکس Drag کنید)", "", s.interCanvas)

	// File List widget for sidebar
	s.fileListView = widget.NewList(
		func() int { return len(s.displayFileList) },
		func() fyne.CanvasObject {
			return widget.NewLabel("file_name.jpg")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			lbl := obj.(*widget.Label)
			if id < len(s.displayFileList) {
				lbl.SetText(filepath.Base(s.displayFileList[id]))
			}
		},
	)
	s.fileListView.OnSelected = func(id widget.ListItemID) {
		s.currentIndex = id
		s.loadImageAtIndex(id)
	}

	s.searchEntry = widget.NewEntry()
	s.searchEntry.SetPlaceHolder("جستجوی نام فایل...")
	s.searchEntry.OnChanged = func(val string) {
		s.searchQuery = val
		s.applyFileFilter()
	}

	s.extFilter = "همه فرمت‌ها"
	s.filterSelect = widget.NewSelect([]string{"همه فرمت‌ها", ".jpg / .jpeg", ".png", ".webp"}, func(val string) {
		s.extFilter = val
		s.applyFileFilter()
	})
	s.filterSelect.SetSelected("همه فرمت‌ها")

	s.sortOrder = "نام (A-Z)"
	s.sortSelect = widget.NewSelect([]string{
		"نام (A-Z)",
		"نام (Z-A)",
		"حجم (کم به زیاد)",
		"حجم (زیاد به کم)",
		"جدیدترین به قدیمی‌ترین",
		"قدیمی‌ترین به جدیدترین",
	}, func(val string) {
		s.sortOrder = val
		s.applyFileFilter()
	})
	s.sortSelect.SetSelected("نام (A-Z)")

	openFilesBtn := widget.NewButtonWithIcon("انتخاب فایل‌ها", theme.FileImageIcon(), s.openNativeFiles)
	openFolderBtn := widget.NewButtonWithIcon("باز کردن پوشه", theme.FolderOpenIcon(), s.openFolderDialog)
	refreshFolderBtn := widget.NewButtonWithIcon("بروزرسانی پوشه (Refresh)", theme.ViewRefreshIcon(), s.refreshFolder)

	filterSortRow := container.NewGridWithColumns(2, s.filterSelect, s.sortSelect)

	folderHeader := container.NewVBox(
		container.NewGridWithColumns(2, openFolderBtn, openFilesBtn),
		refreshFolderBtn,
		s.searchEntry,
		filterSortRow,
	)

	folderBox := container.NewBorder(
		folderHeader,
		nil, nil, nil,
		s.fileListView,
	)

	// Resize section
	s.widthEntry = widget.NewEntry()
	s.widthEntry.SetPlaceHolder("عرض")
	s.heightEntry = widget.NewEntry()
	s.heightEntry.SetPlaceHolder("ارتفاع")
	s.lockAspect = widget.NewCheck("حفظ تناسب ابعاد", nil)
	s.lockAspect.SetChecked(true)

	s.widthEntry.OnChanged = func(val string) {
		if s.updating || !s.lockAspect.Checked || s.origWidth == 0 {
			return
		}
		w, err := strconv.Atoi(val)
		if err != nil || w <= 0 {
			return
		}
		s.updating = true
		h := int(float64(w) * float64(s.origHeight) / float64(s.origWidth))
		s.heightEntry.SetText(strconv.Itoa(h))
		s.updating = false
	}

	s.heightEntry.OnChanged = func(val string) {
		if s.updating || !s.lockAspect.Checked || s.origHeight == 0 {
			return
		}
		h, err := strconv.Atoi(val)
		if err != nil || h <= 0 {
			return
		}
		s.updating = true
		w := int(float64(h) * float64(s.origWidth) / float64(s.origHeight))
		s.widthEntry.SetText(strconv.Itoa(w))
		s.updating = false
	}

	scale50 := widget.NewButton("50%", func() { s.scaleTo(0.5) })
	scale75 := widget.NewButton("75%", func() { s.scaleTo(0.75) })
	scaleOrig := widget.NewButton("100%", func() { s.scaleTo(1.0) })
	quickScale := container.NewGridWithColumns(3, scale50, scale75, scaleOrig)

	resizeForm := widget.NewForm(
		widget.NewFormItem("عرض:", s.widthEntry),
		widget.NewFormItem("ارتفاع:", s.heightEntry),
	)
	resizeCard := widget.NewCard("تغییر سایز", "", container.NewVBox(resizeForm, s.lockAspect, quickScale))

	// Crop section
	s.cropX = widget.NewEntry()
	s.cropY = widget.NewEntry()
	s.cropW = widget.NewEntry()
	s.cropH = widget.NewEntry()

	syncCropFromInputs := func() {
		if s.updating {
			return
		}
		x, _ := strconv.Atoi(s.cropX.Text)
		y, _ := strconv.Atoi(s.cropY.Text)
		w, _ := strconv.Atoi(s.cropW.Text)
		h, _ := strconv.Atoi(s.cropH.Text)
		s.selX = x
		s.selY = y
		s.selW = w
		s.selH = h
		s.cropActive = (w > 0 && h > 0 && (x != 0 || y != 0 || w != s.origWidth || h != s.origHeight))
		s.interCanvas.Refresh()
	}

	s.cropX.OnChanged = func(_ string) { syncCropFromInputs() }
	s.cropY.OnChanged = func(_ string) { syncCropFromInputs() }
	s.cropW.OnChanged = func(_ string) { syncCropFromInputs() }
	s.cropH.OnChanged = func(_ string) { syncCropFromInputs() }

	cropForm := widget.NewForm(
		widget.NewFormItem("X:", s.cropX),
		widget.NewFormItem("Y:", s.cropY),
		widget.NewFormItem("عرض:", s.cropW),
		widget.NewFormItem("ارتفاع:", s.cropH),
	)

	applyCropBtn := widget.NewButtonWithIcon("اعمال کراپ", theme.ContentCutIcon(), s.applyCropDirect)
	applyCropBtn.Importance = widget.WarningImportance

	cropCenterBtn := widget.NewButton("مربع وسط (1:1)", s.cropCenterSquare)
	resetCropBtn := widget.NewButton("ریست برش", s.resetCrop)
	cropCard := widget.NewCard("کراپ (برش)", "", container.NewVBox(cropForm, applyCropBtn, container.NewGridWithColumns(2, cropCenterBtn, resetCropBtn)))

	// Rotate & Flip
	rotLeftBtn := widget.NewButtonWithIcon("۹۰° چپ", theme.NavigateBackIcon(), func() { s.rotate(-90) })
	rotRightBtn := widget.NewButtonWithIcon("۹۰° راست", theme.NavigateNextIcon(), func() { s.rotate(90) })
	flipHBtn := widget.NewButton("معکوس افقی", s.flipH)
	flipVBtn := widget.NewButton("معکوس عمودی", s.flipV)
	toolsCard := widget.NewCard("ابزارها", "", container.NewGridWithColumns(2, rotLeftBtn, rotRightBtn, flipHBtn, flipVBtn))

	// Format & Quality & Save
	s.formatSelect = widget.NewSelect([]string{"JPEG", "PNG"}, nil)
	s.formatSelect.SetSelected("JPEG")

	s.qualityLabel = widget.NewLabel("کیفیت: 85%")
	s.qualitySlider = widget.NewSlider(10, 100)
	s.qualitySlider.SetValue(85)
	s.qualitySlider.Step = 5
	s.qualitySlider.OnChanged = func(val float64) {
		s.qualityLabel.SetText(fmt.Sprintf("کیفیت: %d%%", int(val)))
	}

	s.formatSelect.OnChanged = func(val string) {
		if val == "PNG" {
			s.qualitySlider.Hide()
			s.qualityLabel.Hide()
		} else {
			s.qualitySlider.Show()
			s.qualityLabel.Show()
		}
	}

	saveBtn := widget.NewButtonWithIcon("ذخیره روی فایل اصلی (Save)", theme.DocumentSaveIcon(), s.saveOverwrite)
	saveBtn.Importance = widget.HighImportance

	saveAsBtn := widget.NewButtonWithIcon("ذخیره در فایل جدید (Save As)", theme.DocumentSaveIcon(), s.saveAs)
	batchBtn := widget.NewButtonWithIcon("پردازش دسته‌ای همه به پوشه...", theme.FolderNewIcon(), s.batchProcessAll)

	saveActions := container.NewVBox(
		widget.NewForm(widget.NewFormItem("فرمت:", s.formatSelect)),
		s.qualityLabel,
		s.qualitySlider,
		saveBtn,
		saveAsBtn,
		batchBtn,
	)
	saveCard := widget.NewCard("ذخیره و خروجی", "", saveActions)

	toolsBox := container.NewVScroll(container.NewVBox(
		s.infoLabel,
		resizeCard,
		cropCard,
		toolsCard,
		saveCard,
	))

	splitMain := container.NewHSplit(previewCard, toolsBox)
	splitMain.SetOffset(0.66)

	splitAll := container.NewHSplit(folderBox, splitMain)
	splitAll.SetOffset(0.25)

	s.window.SetContent(splitAll)
}

func (s *AppState) applyFileFilter() {
	q := strings.ToLower(strings.TrimSpace(s.searchQuery))
	extF := s.extFilter

	var filtered []string
	for _, p := range s.fileList {
		base := filepath.Base(p)
		ext := strings.ToLower(filepath.Ext(p))

		// Check ext filter
		extMatch := true
		switch extF {
		case ".jpg / .jpeg":
			extMatch = (ext == ".jpg" || ext == ".jpeg")
		case ".png":
			extMatch = (ext == ".png")
		case ".webp":
			extMatch = (ext == ".webp")
		}

		if !extMatch {
			continue
		}

		// Check search query
		if q != "" && !strings.Contains(strings.ToLower(base), q) {
			continue
		}

		filtered = append(filtered, p)
	}

	// Apply Sorting
	switch s.sortOrder {
	case "نام (A-Z)":
		sort.Slice(filtered, func(i, j int) bool {
			return strings.ToLower(filepath.Base(filtered[i])) < strings.ToLower(filepath.Base(filtered[j]))
		})
	case "نام (Z-A)":
		sort.Slice(filtered, func(i, j int) bool {
			return strings.ToLower(filepath.Base(filtered[i])) > strings.ToLower(filepath.Base(filtered[j]))
		})
	case "حجم (کم به زیاد)":
		sort.Slice(filtered, func(i, j int) bool {
			fi1, _ := os.Stat(filtered[i])
			fi2, _ := os.Stat(filtered[j])
			var s1, s2 int64
			if fi1 != nil {
				s1 = fi1.Size()
			}
			if fi2 != nil {
				s2 = fi2.Size()
			}
			return s1 < s2
		})
	case "حجم (زیاد به کم)":
		sort.Slice(filtered, func(i, j int) bool {
			fi1, _ := os.Stat(filtered[i])
			fi2, _ := os.Stat(filtered[j])
			var s1, s2 int64
			if fi1 != nil {
				s1 = fi1.Size()
			}
			if fi2 != nil {
				s2 = fi2.Size()
			}
			return s1 > s2
		})
	case "جدیدترین به قدیمی‌ترین":
		sort.Slice(filtered, func(i, j int) bool {
			fi1, _ := os.Stat(filtered[i])
			fi2, _ := os.Stat(filtered[j])
			if fi1 == nil || fi2 == nil {
				return false
			}
			return fi1.ModTime().After(fi2.ModTime())
		})
	case "قدیمی‌ترین به جدیدترین":
		sort.Slice(filtered, func(i, j int) bool {
			fi1, _ := os.Stat(filtered[i])
			fi2, _ := os.Stat(filtered[j])
			if fi1 == nil || fi2 == nil {
				return false
			}
			return fi1.ModTime().Before(fi2.ModTime())
		})
	}

	s.displayFileList = filtered
	s.fileListView.Refresh()

	if len(filtered) > 0 {
		targetIdx := 0
		if s.originalPath != "" {
			for i, p := range filtered {
				if p == s.originalPath {
					targetIdx = i
					break
				}
			}
		}
		s.currentIndex = targetIdx
		s.fileListView.UnselectAll()
		s.fileListView.Select(targetIdx)
	} else {
		s.currentIndex = -1
		s.fileListView.UnselectAll()
	}
}

func (s *AppState) refreshFolder() {
	targetDir := s.currentFolder
	if targetDir == "" && s.originalPath != "" {
		targetDir = filepath.Dir(s.originalPath)
	}
	if targetDir == "" {
		dialog.ShowInformation("پوشه‌ای باز نیست", "ابتدا یک پوشه باز کنید تا امکان بروزرسانی وجود داشته باشد.", s.window)
		return
	}
	s.loadDirectory(targetDir)
}

func (s *AppState) syncCropEntries() {
	s.updating = true
	s.cropX.SetText(strconv.Itoa(s.selX))
	s.cropY.SetText(strconv.Itoa(s.selY))
	s.cropW.SetText(strconv.Itoa(s.selW))
	s.cropH.SetText(strconv.Itoa(s.selH))
	s.updating = false
}

func (s *AppState) openFolderDialog() {
	cmd := exec.Command("zenity", "--file-selection", "--directory",
		"--title=انتخاب پوشه تصاویر")

	out, err := cmd.Output()
	var folderPath string
	if err == nil {
		folderPath = strings.TrimSpace(string(out))
	}

	if folderPath == "" {
		return
	}

	s.loadDirectory(folderPath)
}

func (s *AppState) loadDirectory(dirPath string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		dialog.ShowError(err, s.window)
		return
	}

	var validFiles []string
	validExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".bmp": true,
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if validExts[ext] {
			validFiles = append(validFiles, filepath.Join(dirPath, e.Name()))
		}
	}

	sort.Strings(validFiles)

	if len(validFiles) == 0 {
		dialog.ShowInformation("پوشه خالی", "هیچ تصویر سازگاری در این پوشه یافت نشد.", s.window)
		return
	}

	s.currentFolder = dirPath
	s.fileList = validFiles
	s.applyFileFilter()
}

func (s *AppState) openNativeFiles() {
	cmd := exec.Command("zenity", "--file-selection", "--multiple", "--separator=\n",
		"--title=انتخاب عکس‌ها",
		"--file-filter=تصاویر (*.jpg, *.png, *.webp, *.jpeg, *.bmp) | *.jpg *.jpeg *.png *.webp *.bmp *.JPG *.PNG",
		"--file-filter=همه فایل‌ها | *")

	out, err := cmd.Output()
	if err != nil {
		s.openFallbackFyneDialog()
		return
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return
	}

	lines := strings.Split(raw, "\n")
	var validFiles []string
	for _, l := range lines {
		f := strings.TrimSpace(l)
		if f != "" {
			validFiles = append(validFiles, f)
		}
	}

	if len(validFiles) == 0 {
		return
	}

	s.currentFolder = filepath.Dir(validFiles[0])
	s.fileList = validFiles
	s.applyFileFilter()
}

func (s *AppState) openFallbackFyneDialog() {
	fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		path := reader.URI().Path()
		reader.Close()
		s.currentFolder = filepath.Dir(path)
		s.fileList = []string{path}
		s.applyFileFilter()
	}, s.window)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".jpg", ".jpeg", ".png", ".bmp", ".webp"}))
	fd.Show()
}

func (s *AppState) loadImageAtIndex(idx int) {
	if idx < 0 || idx >= len(s.displayFileList) {
		return
	}

	filePath := s.displayFileList[idx]
	f, err := os.Open(filePath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("خطا در باز کردن فایل: %v", err), s.window)
		return
	}
	defer f.Close()

	src, err := imaging.Decode(f)
	if err != nil {
		dialog.ShowError(fmt.Errorf("خطا در پردازش تصویر: %v", err), s.window)
		return
	}

	s.originalPath = filePath
	s.loadedImg = src
	s.origWidth = src.Bounds().Dx()
	s.origHeight = src.Bounds().Dy()

	var sizeMB float64
	if fi, err := os.Stat(filePath); err == nil {
		sizeMB = float64(fi.Size()) / (1024 * 1024)
	}

	s.infoLabel.SetText(fmt.Sprintf("%s\n%d × %d px | %.2f MB",
		filepath.Base(filePath), s.origWidth, s.origHeight, sizeMB))

	s.updating = true
	s.widthEntry.SetText(strconv.Itoa(s.origWidth))
	s.heightEntry.SetText(strconv.Itoa(s.origHeight))
	s.resetCrop()
	s.updating = false

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".png" {
		s.formatSelect.SetSelected("PNG")
	} else {
		s.formatSelect.SetSelected("JPEG")
	}

	s.updatePreview(src)
}

func (s *AppState) updatePreview(img image.Image) {
	if img == nil {
		return
	}
	s.previewImg.Resource = nil
	s.previewImg.File = ""
	s.previewImg.Image = img
	s.previewImg.Refresh()
	s.interCanvas.Refresh()
}

func (s *AppState) scaleTo(factor float64) {
	if s.origWidth == 0 || s.origHeight == 0 {
		return
	}
	s.updating = true
	newW := int(float64(s.origWidth) * factor)
	newH := int(float64(s.origHeight) * factor)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	s.widthEntry.SetText(strconv.Itoa(newW))
	s.heightEntry.SetText(strconv.Itoa(newH))
	s.updating = false
}

func (s *AppState) resetCrop() {
	s.cropActive = false
	s.selX = 0
	s.selY = 0
	s.selW = s.origWidth
	s.selH = s.origHeight
	s.syncCropEntries()
	s.interCanvas.Refresh()
}

func (s *AppState) cropCenterSquare() {
	if s.origWidth == 0 || s.origHeight == 0 {
		return
	}
	size := s.origWidth
	if s.origHeight < size {
		size = s.origHeight
	}
	s.selX = (s.origWidth - size) / 2
	s.selY = (s.origHeight - size) / 2
	s.selW = size
	s.selH = size
	s.cropActive = true
	s.syncCropEntries()
	s.interCanvas.Refresh()
}

func (s *AppState) applyCropDirect() {
	if s.loadedImg == nil || !s.cropActive || s.selW <= 0 || s.selH <= 0 {
		return
	}

	cropRect := image.Rect(s.selX, s.selY, s.selX+s.selW, s.selY+s.selH).Intersect(s.loadedImg.Bounds())
	if cropRect.Empty() {
		return
	}

	s.loadedImg = imaging.Crop(s.loadedImg, cropRect)
	s.origWidth = s.loadedImg.Bounds().Dx()
	s.origHeight = s.loadedImg.Bounds().Dy()

	s.updating = true
	s.widthEntry.SetText(strconv.Itoa(s.origWidth))
	s.heightEntry.SetText(strconv.Itoa(s.origHeight))
	s.resetCrop()
	s.updating = false

	s.updatePreview(s.loadedImg)
}

func (s *AppState) rotate(angle float64) {
	if s.loadedImg == nil {
		return
	}
	s.loadedImg = imaging.Rotate(s.loadedImg, angle, color.Transparent)
	s.origWidth = s.loadedImg.Bounds().Dx()
	s.origHeight = s.loadedImg.Bounds().Dy()

	s.updating = true
	s.widthEntry.SetText(strconv.Itoa(s.origWidth))
	s.heightEntry.SetText(strconv.Itoa(s.origHeight))
	s.resetCrop()
	s.updating = false

	s.updatePreview(s.loadedImg)
}

func (s *AppState) flipH() {
	if s.loadedImg == nil {
		return
	}
	s.loadedImg = imaging.FlipH(s.loadedImg)
	s.updatePreview(s.loadedImg)
}

func (s *AppState) flipV() {
	if s.loadedImg == nil {
		return
	}
	s.loadedImg = imaging.FlipV(s.loadedImg)
	s.updatePreview(s.loadedImg)
}

func (s *AppState) processCurrent() (image.Image, error) {
	if s.loadedImg == nil {
		return nil, fmt.Errorf("عکسی باز نشده است")
	}

	result := s.loadedImg

	if s.cropActive && s.selW > 0 && s.selH > 0 {
		cropRect := image.Rect(s.selX, s.selY, s.selX+s.selW, s.selY+s.selH).Intersect(result.Bounds())
		if !cropRect.Empty() {
			result = imaging.Crop(result, cropRect)
		}
	}

	w, _ := strconv.Atoi(s.widthEntry.Text)
	h, _ := strconv.Atoi(s.heightEntry.Text)
	if w > 0 && h > 0 {
		if w != result.Bounds().Dx() || h != result.Bounds().Dy() {
			result = imaging.Resize(result, w, h, imaging.Lanczos)
		}
	}

	return result, nil
}

func (s *AppState) saveOverwrite() {
	if s.loadedImg == nil || s.originalPath == "" {
		dialog.ShowError(fmt.Errorf("هیچ فایل باز شده‌ای وجود ندارد"), s.window)
		return
	}

	dialog.ShowConfirm("تأیید ذخیره", "تغییرات مستقیماً روی فایل اصلی ذخیره خواهد شد. ادامه می‌دهید؟", func(ok bool) {
		if !ok {
			return
		}

		processed, err := s.processCurrent()
		if err != nil {
			dialog.ShowError(err, s.window)
			return
		}

		if err := s.encodeToFile(processed, s.originalPath); err != nil {
			dialog.ShowError(err, s.window)
			return
		}

		s.loadedImg = processed
		s.origWidth = processed.Bounds().Dx()
		s.origHeight = processed.Bounds().Dy()
		s.resetCrop()
		s.updatePreview(processed)

		fi, _ := os.Stat(s.originalPath)
		var sizeKB float64
		if fi != nil {
			sizeKB = float64(fi.Size()) / 1024
		}

		dialog.ShowInformation("ذخیره شد",
			fmt.Sprintf("فایل اصلی به‌روزرسانی شد:\nحجم: %.1f KB\nابعاد: %d × %d px",
				sizeKB, s.origWidth, s.origHeight), s.window)
	}, s.window)
}

func (s *AppState) saveAs() {
	if s.loadedImg == nil {
		dialog.ShowError(fmt.Errorf("عکسی باز نشده است"), s.window)
		return
	}

	processed, err := s.processCurrent()
	if err != nil {
		dialog.ShowError(err, s.window)
		return
	}

	ext := ".jpg"
	if s.formatSelect.Selected == "PNG" {
		ext = ".png"
	}

	defaultName := "output" + ext
	if s.originalPath != "" {
		base := strings.TrimSuffix(filepath.Base(s.originalPath), filepath.Ext(s.originalPath))
		defaultName = base + "_edited" + ext
	}

	cmd := exec.Command("zenity", "--file-selection", "--save", "--confirm-overwrite",
		fmt.Sprintf("--filename=%s", defaultName),
		"--title=ذخیره با نام جدید (Save As)")

	out, err := cmd.Output()
	var targetPath string
	if err == nil {
		targetPath = strings.TrimSpace(string(out))
	}

	if targetPath == "" {
		return
	}

	if err := s.encodeToFile(processed, targetPath); err != nil {
		dialog.ShowError(err, s.window)
		return
	}

	if s.currentFolder != "" && filepath.Dir(targetPath) == s.currentFolder {
		s.originalPath = targetPath
		s.loadDirectory(s.currentFolder)
	}

	fi, _ := os.Stat(targetPath)
	var sizeKB float64
	if fi != nil {
		sizeKB = float64(fi.Size()) / 1024
	}

	dialog.ShowInformation("ذخیره شد",
		fmt.Sprintf("فایل با موفقیت ذخیره شد:\nحجم: %.1f KB\nابعاد: %d × %d px",
			sizeKB, processed.Bounds().Dx(), processed.Bounds().Dy()), s.window)
}

func (s *AppState) encodeToFile(img image.Image, targetPath string) error {
	outFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("خطا در ایجاد فایل: %v", err)
	}
	defer outFile.Close()

	ext := strings.ToLower(s.formatSelect.Selected)
	if ext == "png" || strings.ToLower(filepath.Ext(targetPath)) == ".png" {
		return png.Encode(outFile, img)
	}

	q := int(s.qualitySlider.Value)
	return jpeg.Encode(outFile, img, &jpeg.Options{Quality: q})
}

func (s *AppState) batchProcessAll() {
	if len(s.fileList) == 0 {
		dialog.ShowError(fmt.Errorf("هیچ عکسی در لیست نیست"), s.window)
		return
	}

	cmd := exec.Command("zenity", "--file-selection", "--directory",
		"--title=پوشه مقصد برای ذخیره دسته‌ای")

	out, err := cmd.Output()
	if err != nil {
		return
	}

	destDir := strings.TrimSpace(string(out))
	if destDir == "" {
		return
	}

	ext := ".jpg"
	if s.formatSelect.Selected == "PNG" {
		ext = ".png"
	}

	targetW, _ := strconv.Atoi(s.widthEntry.Text)
	targetH, _ := strconv.Atoi(s.heightEntry.Text)

	successCount := 0
	for _, srcPath := range s.fileList {
		img, err := imaging.Open(srcPath)
		if err != nil {
			continue
		}

		res := img
		if targetW > 0 && targetH > 0 {
			res = imaging.Resize(img, targetW, targetH, imaging.Lanczos)
		}

		baseName := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
		outPath := filepath.Join(destDir, baseName+"_processed"+ext)

		if err := s.encodeToFile(res, outPath); err == nil {
			successCount++
		}
	}

	dialog.ShowInformation("پردازش دسته‌ای تمام شد",
		fmt.Sprintf("تعداد %d از %d تصویر پردازش و ذخیره شد.", successCount, len(s.fileList)),
		s.window)
}
