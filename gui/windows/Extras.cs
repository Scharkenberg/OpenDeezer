using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using System;
using System.Collections.Generic;
using Windows.Foundation;

namespace OpenDeezer
{
	public class JustifiedWrapPanel : Panel
	{
		public Orientation Orientation { get; set; } = Orientation.Horizontal;
		protected override Size MeasureOverride(Size availableSize)
		{
			double lineThickness = 0;
			double lineLength = 0;
			double totalThickness = 0;
			double maxLength = 0;
			double constraintLength = Orientation == Orientation.Horizontal ? availableSize.Width : availableSize.Height;
			if (double.IsInfinity(constraintLength)) constraintLength = double.MaxValue;

			foreach (var child in Children)
			{
				child.Measure(availableSize);
				var desired = child.DesiredSize;
				double childLength = Orientation == Orientation.Horizontal ? desired.Width : desired.Height;
				double childThickness = Orientation == Orientation.Horizontal ? desired.Height : desired.Width;

				if (lineLength + childLength > constraintLength && lineLength > 0)
				{
					totalThickness += lineThickness;
					maxLength = Math.Max(maxLength, lineLength);
					lineLength = childLength;
					lineThickness = childThickness;
				}
				else
				{
					lineLength += childLength;
					lineThickness = Math.Max(lineThickness, childThickness);
				}
			}

			totalThickness += lineThickness;
			maxLength = Math.Max(maxLength, lineLength);

			return Orientation == Orientation.Horizontal
				? new Size(maxLength, totalThickness)
				: new Size(totalThickness, maxLength);
		}

		protected override Size ArrangeOverride(Size finalSize)
		{
			double constraintLength = Orientation == Orientation.Horizontal ? finalSize.Width : finalSize.Height;
			var lineChildren = new List<UIElement>();
			double lineLength = 0;
			double lineThickness = 0;
			double offsetThickness = 0;

			void ArrangeLine()
			{
				if (lineChildren.Count == 0) return;
				double start = Math.Max(0, (constraintLength - lineLength) / 2.0);
				double pos = start;
				foreach (var child in lineChildren)
				{
					var ds = child.DesiredSize;
					if (Orientation == Orientation.Horizontal)
					{
						child.Arrange(new Rect(pos, offsetThickness, ds.Width, lineThickness));
						pos += ds.Width;
					}
					else
					{
						child.Arrange(new Rect(offsetThickness, pos, lineThickness, ds.Height));
						pos += ds.Height;
					}
				}
				offsetThickness += lineThickness;
				lineChildren.Clear();
				lineLength = 0;
				lineThickness = 0;
			}

			foreach (var child in Children)
			{
				var ds = child.DesiredSize;
				double childLength = Orientation == Orientation.Horizontal ? ds.Width : ds.Height;
				double childThickness = Orientation == Orientation.Horizontal ? ds.Height : ds.Width;

				if (lineLength + childLength > constraintLength && lineLength > 0)
				{
					ArrangeLine();
				}

				lineChildren.Add(child);
				lineLength += childLength;
				lineThickness = Math.Max(lineThickness, childThickness);
			}

			ArrangeLine();
			return finalSize;
		}
	}

}
